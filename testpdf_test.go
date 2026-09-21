package parserails

import (
	"fmt"
	"strings"
)

// textRun is one string drawn on a test page at a given position and size.
// Coordinates are PDF user space: origin bottom-left, Y grows upward.
type textRun struct {
	Text string
	X, Y float64
	Size float64
}

const (
	testPageWidth  = 612.0
	testPageHeight = 792.0
)

// pdfWithPages builds a valid multi-page PDF drawing the given runs, computing
// real xref offsets so PDFium can parse it. Tests use it instead of binary
// fixtures so the geometry under test is visible in the test itself.
func pdfWithPages(pages [][]textRun) []byte {
	var (
		objects  []string
		pageRefs []string
	)
	// Object numbering: 1 = catalog, 2 = page tree, 3 = font, then two objects
	// (page, content) per page.
	objects = append(objects,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"", // placeholder, page tree needs the kid references below
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	)
	for _, runs := range pages {
		var content strings.Builder
		for _, r := range runs {
			size := r.Size
			if size == 0 {
				size = 12
			}
			fmt.Fprintf(&content, "BT /F1 %g Tf %g %g Td (%s) Tj ET\n",
				size, r.X, r.Y, escapePDFString(r.Text))
		}
		contentNum := len(objects) + 2
		objects = append(objects, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %g %g] /Contents %d 0 R "+
				"/Resources << /Font << /F1 3 0 R >> >> >>",
			testPageWidth, testPageHeight, contentNum))
		pageRefs = append(pageRefs, fmt.Sprintf("%d 0 R", len(objects)))
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream",
			content.Len(), content.String()))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>",
		strings.Join(pageRefs, " "), len(pages))

	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, body := range objects {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", len(objects)+1)
	b.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF",
		len(objects)+1, xref)
	return []byte(b.String())
}

// pdfWithRuns builds a single-page PDF.
func pdfWithRuns(runs ...textRun) []byte { return pdfWithPages([][]textRun{runs}) }

// minimalPDF builds a single-page PDF rendering text at (100,700).
func minimalPDF(text string) []byte {
	return pdfWithRuns(textRun{Text: text, X: 100, Y: 700, Size: 24})
}

func escapePDFString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	return r.Replace(s)
}
