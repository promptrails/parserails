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
	var stopped int
	node.Walk(func(n *Node) {
		if n.Err != nil && strings.Contains(n.Err.Error(), "more than 3 files") {
			stopped++
		}
	})
	if stopped == 0 {
		t.Fatalf("file limit never fired: %s", treeSummary(node))
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
