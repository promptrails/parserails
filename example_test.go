package parserails_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/promptrails/parserails"
	"github.com/promptrails/parserails/ocr/tesseract"
)

// Create one Parser per process and reuse it: it owns a pooled PDFium runtime
// and is safe for concurrent use.
func ExampleParser_Parse() {
	p, err := parserails.New()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	doc, err := p.Parse(context.Background(), pdfBytes)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, w := range doc.Words() {
		fmt.Printf("p%d %q [%.1f %.1f %.1f %.1f]\n", w.Page, w.Text, w.X0, w.Y0, w.X1, w.Y1)
	}
}

// ParseFile detects the format from the file's content, not its extension,
// and converts office documents through LibreOffice.
func ExampleParser_ParseFile() {
	p, err := parserails.New()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	doc, err := p.ParseFile(context.Background(), "quarterly.docx")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(doc.Text())
}

// ReadOptions carries a password and a page range, so one parser can serve
// documents that need different ones.
func ExampleParser_ParseData() {
	p, err := parserails.New()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	doc, err := p.ParseData(context.Background(), sealedPDF, parserails.ReadOptions{
		Name:     "statement.pdf",
		Password: "hunter2",
		Pages:    "1-5,10",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(doc.Pages), "pages read")
}

// Inspect is a text-layer pass: no page is rendered and no OCR runs, so a
// pipeline can route or price a batch for a fraction of a parse.
func ExampleParser_Inspect() {
	p, err := parserails.New()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	complexity, err := p.Inspect(context.Background(), pdfBytes, parserails.ReadOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	if complexity.NeedsOCR() {
		fmt.Println("pages needing OCR:", complexity.OCRPages())
		for _, page := range complexity.Pages {
			if page.NeedsOCR {
				fmt.Printf("  page %d: %v\n", page.Page, page.Reasons)
			}
		}
	}
}

// Extract walks a file and everything inside it. One unreadable attachment is
// recorded on its own node instead of failing the walk.
func ExampleParser_Extract() {
	p, err := parserails.New()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	node, err := p.ExtractFile(context.Background(), "bundle.zip", parserails.ExtractOptions{
		MaxDepth: 4,
		MaxBytes: 64 << 20,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	node.Walk(func(n *parserails.Node) {
		switch {
		case n.Err != nil:
			fmt.Printf("%s [%s] not read: %v\n", n.Name, n.Format, n.Err)
		case n.Document != nil:
			fmt.Printf("%s [%s] %d page(s)\n", n.Name, n.Format, len(n.Document.Pages))
		}
	})
}

// A scan has no text layer, so reading one needs an OCR backend. The result
// is a one-page document whose page is the image.
func ExampleParser_ParseImage() {
	p, err := parserails.New(
		parserails.WithOCR(tesseract.New(tesseract.Config{Lang: "eng"})),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	doc, err := p.ParseImage(context.Background(), jpegBytes)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(doc.Text())
}

// WithOCR sets the fallback for pages with no extractable text; WithImageOCR
// additionally reads the figures on pages that do have some.
func ExampleWithImageOCR() {
	p, err := parserails.New(
		parserails.WithOCR(tesseract.New(tesseract.Config{Lang: "eng"})),
		parserails.WithImageOCR(),
		parserails.WithTimeout(30*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	doc, err := p.Parse(context.Background(), pdfBytes)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, w := range doc.Words() {
		if w.IsOCR() && w.Confidence < 0.6 {
			fmt.Printf("low confidence: %q (%.2f)\n", w.Text, w.Confidence)
		}
	}
}

// A container yields one level of embedded files; ParseRails walks the tree
// and applies the limits. Register one for a format it does not know — or
// replace a built-in.
func ExampleWithContainer() {
	p, err := parserails.New(parserails.WithContainer(parserails.FormatZIP, noContainer{}))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	// Archives are now left unopened.
	node, err := p.Extract(context.Background(), zipBytes, parserails.ExtractOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(node.Children), "children")
}

// noContainer unpacks nothing, which switches extraction off for a format.
type noContainer struct{}

func (noContainer) Children(context.Context, []byte, parserails.ChildRequest) ([]parserails.Child, error) {
	return nil, nil
}

// Sniff identifies a document from its leading bytes. Extensions lie: a DOCX
// saved as .pdf, or a byte slice with no name at all, are routine.
func ExampleSniff() {
	fmt.Println(parserails.Sniff([]byte("%PDF-1.7\n...")))
	fmt.Println(parserails.Sniff([]byte("\x89PNG\r\n\x1a\n")))
	fmt.Println(parserails.Sniff([]byte("BMI,weight\n22,70\n")))
	// Output:
	// pdf
	// png
	// txt
}

// Detect prefers what the bytes say, and consults the name only where they
// are ambiguous: .doc, .xls, .ppt and .msg share one signature.
func ExampleDetect() {
	compoundFile := []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1, 0x00}

	fmt.Println(parserails.Detect("letter.doc", compoundFile))
	fmt.Println(parserails.Detect("mail.msg", compoundFile))
	fmt.Println(parserails.Detect("", compoundFile))
	// Output:
	// doc
	// msg
	// ole
}

// Text is built from lines reconstructed out of the word geometry: lines are
// joined with newlines and pages separated by a form feed.
func ExampleDocument_Text() {
	doc := &parserails.Document{Pages: []parserails.Page{{
		Index: 0, Width: 612, Height: 792,
		Words: []parserails.Word{
			{Text: "Invoice", X0: 72, Y0: 700, X1: 120, Y1: 712},
			{Text: "2026-03", X0: 124, Y0: 700, X1: 180, Y1: 712},
			{Text: "Total", X0: 72, Y0: 680, X1: 110, Y1: 692},
			{Text: "42.00", X0: 114, Y0: 680, X1: 150, Y1: 692},
		},
	}}}

	fmt.Println(doc.Text())
	// Output:
	// Invoice 2026-03
	// Total 42.00
}

// Markdown renders the blocks ParseRails reconstructs from the layout. Here
// the larger line becomes a heading and the two aligned rows a table.
func ExampleDocument_Markdown() {
	doc := &parserails.Document{Pages: []parserails.Page{{
		Index: 0, Width: 612, Height: 792,
		Words: []parserails.Word{
			{Text: "Revenue", X0: 72, Y0: 720, X1: 200, Y1: 744, FontSize: 24},
			{Text: "Region", X0: 72, Y0: 700, X1: 110, Y1: 711, FontSize: 11},
			{Text: "Total", X0: 220, Y0: 700, X1: 250, Y1: 711, FontSize: 11},
			{Text: "EMEA", X0: 72, Y0: 684, X1: 108, Y1: 695, FontSize: 11},
			{Text: "1.2M", X0: 220, Y0: 684, X1: 248, Y1: 695, FontSize: 11},
		},
	}}}

	fmt.Print(doc.Markdown())
	// Output:
	// # Revenue
	//
	// | Region | Total |
	// | --- | --- |
	// | EMEA | 1.2M |
}

// ReadOfficeDocument reads an Office Open XML package directly: no
// LibreOffice, no PDF in between, and the document's own structure instead of
// one inferred from a layout.
func ExampleReadOfficeDocument() {
	office, err := parserails.ReadOfficeDocument(docxBytes(), parserails.FormatDOCX)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(office.Markdown())
	// Output:
	// # Quarterly Report
	//
	// Revenue grew across every region.
}

// docxBytes builds a minimal DOCX package, so the example above can run.
func docxBytes() []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := w.Write([]byte(`<w:document xmlns:w="x"><w:body>` +
		`<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>Quarterly Report</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Revenue grew across every region.</w:t></w:r></w:p>` +
		`</w:body></w:document>`)); err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

// Stand-ins for documents the examples above would read from disk.
var (
	pdfBytes  []byte
	sealedPDF []byte
	jpegBytes []byte
	zipBytes  []byte
)
