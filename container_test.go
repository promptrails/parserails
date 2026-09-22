package parserails

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestExtractWalksZipArchives(t *testing.T) {
	p := newTestParser(t)
	archive := zipArchive(map[string][]byte{
		"invoice.pdf": minimalPDF("Invoice total 42"),
		"notes.txt":   []byte("a plain note"),
	})

	node, err := p.Extract(context.Background(), archive, ExtractOptions{ReadOptions: ReadOptions{Name: "bundle.zip"}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if node.Format != FormatZIP || len(node.Children) != 2 {
		t.Fatalf("tree = %+v", node)
	}

	byName := map[string]*Node{}
	for _, c := range node.Children {
		byName[c.Name] = c
	}
	pdf := byName["invoice.pdf"]
	if pdf == nil || pdf.Document == nil {
		t.Fatalf("pdf child not parsed: %+v", byName)
	}
	if got := pdf.Document.Text(); got != "Invoice total 42" {
		t.Errorf("pdf text = %q", got)
	}
	if got := byName["notes.txt"].Text; got != "a plain note" {
		t.Errorf("text child = %q", got)
	}
	if all := node.AllText(); !strings.Contains(all, "Invoice total 42") || !strings.Contains(all, "a plain note") {
		t.Errorf("AllText = %q, want both children", all)
	}
}

func TestExtractNestsAndRespectsMaxDepth(t *testing.T) {
	p := newTestParser(t)
	inner := zipArchive(map[string][]byte{"deep.pdf": minimalPDF("Deep")})
	outer := zipArchive(map[string][]byte{"inner.zip": inner})

	node, err := p.Extract(context.Background(), outer, ExtractOptions{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if node.Count() != 3 { // outer + inner.zip + deep.pdf
		t.Fatalf("tree has %d files, want 3: %s", node.Count(), treeSummary(node))
	}

	shallow, err := p.Extract(context.Background(), outer, ExtractOptions{MaxDepth: 1})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if shallow.Count() != 2 { // the walk stops at inner.zip
		t.Fatalf("depth-limited tree = %s", treeSummary(shallow))
	}
}

func TestExtractStopsOnRepeatedContent(t *testing.T) {
	p := newTestParser(t)
	pdf := minimalPDF("Same bytes")
	archive := zipArchive(map[string][]byte{"a.pdf": pdf, "b.pdf": pdf})

	node, err := p.Extract(context.Background(), archive, ExtractOptions{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var parsed, skipped int
	for _, c := range node.Children {
		if c.Document != nil {
			parsed++
		}
		if c.Err != nil && strings.Contains(c.Err.Error(), "already extracted") {
			skipped++
		}
	}
	if parsed != 1 || skipped != 1 {
		t.Fatalf("parsed=%d skipped=%d, want the duplicate to be visited once: %s", parsed, skipped, treeSummary(node))
	}
}

func TestExtractStopsUnpackingAtTheByteBudget(t *testing.T) {
	p := newTestParser(t)
	// Six entries of 4 MiB of zeros each: tiny compressed, 24 MiB unpacked.
	files := map[string][]byte{}
	for i := 0; i < 6; i++ {
		files[fmt.Sprintf("big-%d.bin", i)] = make([]byte, 4<<20)
	}
	node, err := p.Extract(context.Background(), zipArchive(files), ExtractOptions{MaxBytes: 5 << 20})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var taken, refused int
	for _, c := range node.Children {
		if c.Err != nil {
			refused++
			continue
		}
		taken++
	}
	if refused == 0 {
		t.Fatalf("no entry was refused under a 5 MiB budget: %s", treeSummary(node))
	}
	if taken > 2 {
		t.Errorf("unpacked %d entries of 4 MiB under a 5 MiB budget", taken)
	}
}

func TestZipEntriesTreatAnExhaustedBudgetAsExhausted(t *testing.T) {
	// Zero means "no limit" in the contract, so a budget that runs out must
	// not be handed on as one.
	archive := zipArchive(map[string][]byte{
		"a.bin": make([]byte, 4),
		"b.bin": make([]byte, 4),
	})
	children, err := zipEntries(archive, ChildRequest{MaxBytes: 4}, func(string) bool { return true })
	if err != nil {
		t.Fatalf("zipEntries: %v", err)
	}
	var unpacked int
	for _, c := range children {
		unpacked += len(c.Data)
	}
	if unpacked > 4 {
		t.Fatalf("unpacked %d bytes under a 4 byte budget", unpacked)
	}
}

func TestPDFAttachmentsRespectTheBudget(t *testing.T) {
	p := newTestParser(t)
	pdf := pdfWithAttachment("payload.bin", bytes.Repeat([]byte("A"), 64))

	children, err := pdfContainer{parser: p}.Children(context.Background(), pdf, ChildRequest{MaxBytes: 8})
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	for _, c := range children {
		if len(c.Data) > 8 {
			t.Fatalf("returned %d bytes under an 8 byte budget", len(c.Data))
		}
		if c.Err == nil {
			t.Errorf("the oversized attachment should be reported, not dropped: %+v", c)
		}
	}
}

func TestZipRefusalsSpendTheFileBudget(t *testing.T) {
	// A small MaxFiles must bound what is attempted, not only what is kept:
	// otherwise every entry of a huge archive is still opened and refused.
	files := map[string][]byte{}
	for i := 0; i < 20; i++ {
		files[fmt.Sprintf("big-%02d.bin", i)] = make([]byte, 1024)
	}
	children, err := zipEntries(zipArchive(files), ChildRequest{MaxFiles: 1, MaxBytes: 1},
		func(string) bool { return true })
	if err != nil {
		t.Fatalf("zipEntries: %v", err)
	}
	if len(children) > 2 {
		t.Fatalf("attempted %d entries with MaxFiles=1", len(children))
	}
}

func TestNestedArchivesShareOneBudget(t *testing.T) {
	p := newTestParser(t)
	// Each level holds a megabyte. Charging a level only when the walk
	// reaches it would hand every level the whole budget again.
	inner := zipArchive(map[string][]byte{"inner.bin": make([]byte, 1<<20)})
	outer := zipArchive(map[string][]byte{"inner.zip": inner, "outer.bin": make([]byte, 1<<20)})

	node, err := p.Extract(context.Background(), outer, ExtractOptions{MaxBytes: 1536 << 10})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var unpacked int
	node.Walk(func(n *Node) {
		if n.Document != nil {
			unpacked += len(n.Document.Pages)
		}
	})
	var refused int
	node.Walk(func(n *Node) {
		if n.Err != nil {
			refused++
		}
	})
	if refused == 0 {
		t.Fatalf("nothing was refused under a 1.5 MiB budget: %s", treeSummary(node))
	}
}

func TestMailAttachmentsAreMeasuredDecoded(t *testing.T) {
	// MaxBytes counts unpacked bytes; base64 is a third larger than what it
	// carries, so measuring the encoded form refuses attachments that fit.
	eml := emailWithAttachment("subject", "body", "small.bin", []byte("123456"))
	children, err := (emlContainer{}).Children(context.Background(), eml, ChildRequest{MaxBytes: 6})
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(children) != 1 || children[0].Err != nil {
		t.Fatalf("children = %+v, want the 6-byte attachment accepted", children)
	}
}

func TestUnnamedMailPartsAreBudgeted(t *testing.T) {
	// An inline application/octet-stream is an attachment in everything but
	// its headers.
	var b bytes.Buffer
	b.WriteString("From: a@b.c\r\nSubject: hi\r\nMIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=B\r\n\r\n")
	b.WriteString("--B\r\nContent-Type: text/plain\r\n\r\nbody\r\n")
	b.WriteString("--B\r\nContent-Type: application/octet-stream\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	b.WriteString(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("X"), 64)))
	b.WriteString("\r\n--B--\r\n")

	children, err := (emlContainer{}).Children(context.Background(), b.Bytes(), ChildRequest{MaxBytes: 8})
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	for _, c := range children {
		if len(c.Data) > 8 {
			t.Fatalf("part %q is %d bytes under an 8 byte budget", c.Name, len(c.Data))
		}
		if c.Err == nil {
			t.Errorf("the refused part should carry a reason: %+v", c)
		}
	}
}

func TestOversizedMSGAttachmentIsReportedNotDropped(t *testing.T) {
	msg := buildCFB([]cfbTestEntry{
		{Name: "__substg1.0_0037001F", Data: utf16Bytes("subject")},
		{Name: "__attach_version1.0_#00000000", Children: []cfbTestEntry{
			{Name: "__substg1.0_3707001F", Data: utf16Bytes("big.bin")},
			{Name: "__substg1.0_37010102", Data: bytes.Repeat([]byte("A"), 64)},
		}},
	})
	children, err := (msgContainer{}).Children(context.Background(), msg, ChildRequest{MaxBytes: 8})
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(children) != 1 || children[0].Err == nil {
		t.Fatalf("children = %+v, want the attachment reported with a reason", children)
	}
	if children[0].Name != "big.bin" {
		t.Errorf("name = %q, want the attachment's own", children[0].Name)
	}
}

func TestOlePackageHeaderIsNotChargedToThePayload(t *testing.T) {
	// The stream carries the original and temporary paths ahead of the
	// payload; the budget is about what comes out of it.
	ole := buildCFB([]cfbTestEntry{
		{Name: "\x01Ole10Native", Data: ole10Native("tiny.png", []byte("\x89PNG\r\n\x1a\n"))},
	})
	children, err := (oleContainer{}).Children(context.Background(), ole, ChildRequest{MaxBytes: 16})
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(children) != 1 || children[0].Err != nil {
		t.Fatalf("children = %+v, want the small payload accepted", children)
	}
}

func TestQuotedPrintableAttachmentsAreNotTruncated(t *testing.T) {
	// Quoted-printable triples a byte in the worst case: measuring the
	// encoded form against the budget silently cut attachments short.
	payload := []byte("ÄÖÜ ready")
	var b bytes.Buffer
	b.WriteString("From: a@b.c\r\nSubject: s\r\nMIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=B\r\n\r\n")
	b.WriteString("--B\r\nContent-Type: application/octet-stream\r\n")
	b.WriteString("Content-Disposition: attachment; filename=\"note.bin\"\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	for _, c := range payload {
		fmt.Fprintf(&b, "=%02X", c)
	}
	b.WriteString("\r\n--B--\r\n")

	children, err := (emlContainer{}).Children(context.Background(), b.Bytes(),
		ChildRequest{MaxBytes: int64(len(payload))})
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(children) != 1 || children[0].Err != nil {
		t.Fatalf("children = %+v, want the attachment accepted whole", children)
	}
	if !bytes.Equal(children[0].Data, payload) {
		t.Fatalf("data = %q, want %q", children[0].Data, payload)
	}
}

func TestNestedMessagesAreBudgeted(t *testing.T) {
	msg := buildCFB([]cfbTestEntry{
		{Name: "__substg1.0_0037001F", Data: utf16Bytes("outer")},
		{Name: "__attach_version1.0_#00000000", Children: []cfbTestEntry{
			{Name: "__substg1.0_3701000D", Children: []cfbTestEntry{
				{Name: "__substg1.0_1000001F", Data: utf16Bytes("a body well over the budget")},
			}},
		}},
	})
	children, err := (msgContainer{}).Children(context.Background(), msg, ChildRequest{MaxBytes: 4})
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	for _, c := range children {
		if len(c.Data) > 4 {
			t.Fatalf("emitted %d bytes under a 4 byte budget", len(c.Data))
		}
		if c.Err == nil {
			t.Errorf("the refused message should carry a reason: %+v", c)
		}
	}
}

func TestFileSlotsAreReservedForSiblings(t *testing.T) {
	p := newTestParser(t)
	inner := zipArchive(map[string][]byte{
		"i1.txt": []byte("one"), "i2.txt": []byte("two"), "i3.txt": []byte("three"),
	})
	outer := zipArchive(map[string][]byte{
		"a.zip": inner, "b.txt": []byte("b"), "c.txt": []byte("c"), "d.txt": []byte("d"),
	})

	node, err := p.Extract(context.Background(), outer, ExtractOptions{MaxFiles: 5})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	opened := 0
	node.Walk(func(n *Node) {
		if n.Err == nil {
			opened++
		}
	})
	if opened > 5 {
		t.Fatalf("opened %d files under a limit of 5:\n%s", opened, treeSummary(node))
	}
}

func TestExtractForwardsThePasswordToAttachments(t *testing.T) {
	p := newTestParser(t)
	node, err := p.Extract(context.Background(), encryptedPDF("Secret Report", "hunter2"),
		ExtractOptions{ReadOptions: ReadOptions{Name: "sealed.pdf", Password: "hunter2"}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if node.Err != nil {
		t.Fatalf("node error = %v, want none: the password was given", node.Err)
	}
	if node.Document == nil || node.Document.Text() != "Secret Report" {
		t.Fatalf("document = %+v", node.Document)
	}
}

func TestFileQuotaKeepsWhatItAccepted(t *testing.T) {
	// The quota allows the root plus two entries. Those two must be read:
	// reserving the whole batch at once, refusal markers included, used to
	// refuse the siblings that had already been accepted.
	p := newTestParser(t)
	archive := zipArchive(map[string][]byte{
		"a.txt": []byte("one"), "b.txt": []byte("two"), "c.txt": []byte("three"),
	})

	node, err := p.Extract(context.Background(), archive, ExtractOptions{MaxFiles: 3})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	read := 0
	node.Walk(func(n *Node) {
		if n.Text != "" {
			read++
		}
	})
	if read != 2 {
		t.Fatalf("read %d files, want the two the quota allowed:\n%s", read, treeSummary(node))
	}
}

func TestOneEmbeddedMessageCostsOneFileSlot(t *testing.T) {
	// A forwarded message is assembled from several property streams; it is
	// still one file.
	msg := buildCFB([]cfbTestEntry{
		{Name: "__attach_version1.0_#00000000", Children: []cfbTestEntry{
			{Name: "__substg1.0_3701000D", Children: []cfbTestEntry{
				{Name: "__substg1.0_0037001F", Data: utf16Bytes("Hi")},
				{Name: "__substg1.0_1000001F", Data: utf16Bytes("body")},
			}},
		}},
	})
	children, err := (msgContainer{}).Children(context.Background(), msg,
		ChildRequest{MaxFiles: 1, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(children) != 1 || children[0].Err != nil {
		t.Fatalf("children = %+v, want the message accepted under MaxFiles=1", children)
	}
}

func TestEmbeddedMessageIsBudgetedByWhatItEmits(t *testing.T) {
	// "Subject: Hi" is longer than the property it was rendered from.
	msg := buildCFB([]cfbTestEntry{
		{Name: "__attach_version1.0_#00000000", Children: []cfbTestEntry{
			{Name: "__substg1.0_3701000D", Children: []cfbTestEntry{
				{Name: "__substg1.0_0037001F", Data: utf16Bytes("Hi")},
			}},
		}},
	})
	children, err := (msgContainer{}).Children(context.Background(), msg, ChildRequest{MaxBytes: 4})
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	for _, c := range children {
		if len(c.Data) > 4 {
			t.Fatalf("emitted %d bytes under a 4 byte budget", len(c.Data))
		}
		if c.Err == nil {
			t.Errorf("the refused message should carry a reason: %+v", c)
		}
	}
}

func TestExtractLimitsFileCount(t *testing.T) {
	p := newTestParser(t)
	files := map[string][]byte{}
	for i := 0; i < 6; i++ {
		files[fmt.Sprintf("note-%d.txt", i)] = []byte(fmt.Sprintf("note %d", i))
	}
	node, err := p.Extract(context.Background(), zipArchive(files), ExtractOptions{MaxFiles: 3})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var stopped, taken int
	node.Walk(func(n *Node) {
		switch {
		case n.Err != nil && strings.Contains(n.Err.Error(), "extraction stopped"):
			stopped++
		case n.Text != "":
			taken++
		}
	})
	if stopped == 0 {
		t.Fatalf("file limit never fired: %s", treeSummary(node))
	}
	if taken > 3 {
		t.Errorf("opened %d files under a limit of 3: %s", taken, treeSummary(node))
	}
}

func TestExtractReadsEmailWithAttachment(t *testing.T) {
	p := newTestParser(t)
	eml := emailWithAttachment("Quarterly numbers", "Here is the report.", "report.pdf", minimalPDF("Report body"))

	node, err := p.Extract(context.Background(), eml, ExtractOptions{ReadOptions: ReadOptions{Name: "mail.eml"}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if node.Format != FormatEML {
		t.Fatalf("format = %s, want eml", node.Format)
	}
	if !strings.Contains(node.Text, "Subject: Quarterly numbers") || !strings.Contains(node.Text, "Here is the report.") {
		t.Errorf("mail text = %q", node.Text)
	}
	if len(node.Children) != 1 {
		t.Fatalf("children = %+v, want the attachment", node.Children)
	}
	att := node.Children[0]
	if att.Name != "report.pdf" || att.Document == nil {
		t.Fatalf("attachment = %+v", att)
	}
	if got := att.Document.Text(); got != "Report body" {
		t.Errorf("attachment text = %q", got)
	}
}

func TestExtractUnwrapsOle10Native(t *testing.T) {
	p := newTestParser(t)
	pdf := minimalPDF("Embedded by Windows")
	ole := buildCFB([]cfbTestEntry{
		{Name: "\x01Ole10Native", Data: ole10Native("report.pdf", pdf)},
		{Name: "WordDocument", Data: []byte("not a standalone file")},
	})

	node, err := p.Extract(context.Background(), ole, ExtractOptions{ReadOptions: ReadOptions{Name: "embedded.bin"}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(node.Children) != 1 {
		t.Fatalf("children = %s, want just the package payload", treeSummary(node))
	}
	child := node.Children[0]
	if child.Name != "report.pdf" || child.Document == nil {
		t.Fatalf("child = %+v", child)
	}
	if got := child.Document.Text(); got != "Embedded by Windows" {
		t.Errorf("payload text = %q", got)
	}
}

func TestExtractReadsOutlookMessages(t *testing.T) {
	p := newTestParser(t)
	msg := buildCFB([]cfbTestEntry{
		{Name: "__substg1.0_0037001F", Data: utf16Bytes("Signed contract")},
		{Name: "__substg1.0_1000001F", Data: utf16Bytes("Please countersign the attached.")},
		{Name: "__substg1.0_0042001F", Data: utf16Bytes("legal@example.com")},
		{Name: "__attach_version1.0_#00000000", Children: []cfbTestEntry{
			{Name: "__substg1.0_3707001F", Data: utf16Bytes("contract.pdf")},
			{Name: "__substg1.0_37010102", Data: minimalPDF("Contract text")},
		}},
	})

	node, err := p.Extract(context.Background(), msg, ExtractOptions{ReadOptions: ReadOptions{Name: "mail.msg"}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if node.Format != FormatMSG {
		t.Fatalf("format = %s, want msg", node.Format)
	}
	if !strings.Contains(node.Text, "Subject: Signed contract") ||
		!strings.Contains(node.Text, "From: legal@example.com") ||
		!strings.Contains(node.Text, "Please countersign") {
		t.Errorf("message text = %q", node.Text)
	}
	if len(node.Children) != 1 || node.Children[0].Name != "contract.pdf" {
		t.Fatalf("children = %s", treeSummary(node))
	}
	if got := node.Children[0].Document.Text(); got != "Contract text" {
		t.Errorf("attachment text = %q", got)
	}
}

func TestExtractReadsAMessageAttachedToAMessage(t *testing.T) {
	p := newTestParser(t)
	// "Forward as attachment": the attachment's data property is a storage
	// holding the nested message's own property set, not a stream.
	msg := buildCFB([]cfbTestEntry{
		{Name: "__substg1.0_0037001F", Data: utf16Bytes("Fwd: contract")},
		{Name: "__substg1.0_1000001F", Data: utf16Bytes("See below.")},
		{Name: "__attach_version1.0_#00000000", Children: []cfbTestEntry{
			{Name: "__substg1.0_3707001F", Data: utf16Bytes("original.msg")},
			{Name: "__substg1.0_3701000D", Children: []cfbTestEntry{
				{Name: "__substg1.0_0037001F", Data: utf16Bytes("Signed contract")},
				{Name: "__substg1.0_1000001F", Data: utf16Bytes("Inner body text")},
			}},
		}},
	})

	node, err := p.Extract(context.Background(), msg, ExtractOptions{ReadOptions: ReadOptions{Name: "fwd.msg"}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(node.Children) != 1 {
		t.Fatalf("children = %s", treeSummary(node))
	}
	child := node.Children[0]
	// The nested message's text comes out; its raw property streams do not
	// stand in for the attachment's contents.
	if !strings.Contains(child.Text, "Signed contract") || !strings.Contains(child.Text, "Inner body text") {
		t.Fatalf("nested message = %q", child.Text)
	}
	if child.Format != FormatText {
		t.Errorf("format = %s, want txt", child.Format)
	}
}

func TestExtractReadsOOXMLEmbeddings(t *testing.T) {
	p := newTestParser(t)
	docx := zipArchive(map[string][]byte{
		"word/document.xml":              []byte("<xml/>"),
		"word/embeddings/oleObject1.pdf": minimalPDF("Pasted in"),
		"word/media/image1.png":          samplePNG(4, 4), // not an embedding
	})

	node, err := p.Extract(context.Background(), docx, ExtractOptions{
		ReadOptions: ReadOptions{Name: "report.docx"},
		SkipParse:   true, // no LibreOffice needed: we only want the inventory
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(node.Children) != 1 || !strings.HasSuffix(node.Children[0].Name, "oleObject1.pdf") {
		t.Fatalf("children = %s, want only the embedded object", treeSummary(node))
	}
}

func TestExtractReadsPDFAttachments(t *testing.T) {
	p := newTestParser(t)
	pdf := pdfWithAttachment("invoice.xml", []byte("<invoice><total>42</total></invoice>"))

	node, err := p.Extract(context.Background(), pdf, ExtractOptions{ReadOptions: ReadOptions{Name: "einvoice.pdf"}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if node.Document == nil || !strings.Contains(node.Document.Text(), "Cover page") {
		t.Errorf("host document not parsed: %+v", node.Document)
	}
	if len(node.Children) != 1 {
		t.Fatalf("children = %s, want the embedded file", treeSummary(node))
	}
	child := node.Children[0]
	if child.Name != "invoice.xml" {
		t.Errorf("attachment name = %q", child.Name)
	}
	if !strings.Contains(child.Text, "<total>42</total>") {
		t.Errorf("attachment text = %q", child.Text)
	}
}

func TestNodeJSONKeepsErrors(t *testing.T) {
	node := &Node{Name: "broken.pdf", Format: FormatPDF, Err: fmt.Errorf("boom")}
	raw, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"error":"boom"`) {
		t.Fatalf("json = %s, want the error", raw)
	}
}

// zipArchive builds a ZIP archive from name → contents.
func zipArchive(files map[string][]byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write(body); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// emailWithAttachment builds a multipart/mixed message.
func emailWithAttachment(subject, body, filename string, attachment []byte) []byte {
	const boundary = "BOUNDARY42"
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: sender@example.com\r\nTo: you@example.com\r\nSubject: %s\r\n", subject)
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%s\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", boundary, body)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: application/pdf\r\n", boundary)
	fmt.Fprintf(&b, "Content-Disposition: attachment; filename=%q\r\nContent-Transfer-Encoding: base64\r\n\r\n", filename)
	b.WriteString(base64.StdEncoding.EncodeToString(attachment))
	fmt.Fprintf(&b, "\r\n--%s--\r\n", boundary)
	return b.Bytes()
}

// ole10Native builds the package stream Windows writes for a dropped file.
func ole10Native(filename string, payload []byte) []byte {
	var b bytes.Buffer
	b.Write(make([]byte, 4)) // total size, filled in below
	_ = binary.Write(&b, binary.LittleEndian, uint16(2))
	b.WriteString(filename)
	b.WriteByte(0)
	b.WriteString(`C:\Users\someone\` + filename)
	b.WriteByte(0)
	b.Write(make([]byte, 8)) // two reserved longs
	b.WriteString(`C:\Temp\` + filename)
	b.WriteByte(0)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(payload)))
	b.Write(payload)

	out := b.Bytes()
	binary.LittleEndian.PutUint32(out[:4], uint32(len(out)-4))
	return out
}

func treeSummary(n *Node) string {
	var b strings.Builder
	var walk func(*Node, int)
	walk = func(node *Node, depth int) {
		fmt.Fprintf(&b, "%s%s [%s]", strings.Repeat("  ", depth), node.Name, node.Format)
		if node.Err != nil {
			fmt.Fprintf(&b, " err=%v", node.Err)
		}
		b.WriteString("\n")
		for _, c := range node.Children {
			walk(c, depth+1)
		}
	}
	walk(n, 0)
	return b.String()
}
