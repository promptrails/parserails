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

// imageBox is a raster image drawn on a test page, in the same coordinates.
type imageBox struct {
	X, Y, W, H float64
}

// pdfPage describes one page of a test document.
type pdfPage struct {
	Runs   []textRun
	Images []imageBox
}

const (
	testPageWidth  = 612.0
	testPageHeight = 792.0
)

// pdfWithPageSpecs builds a valid multi-page PDF, computing real xref offsets
// so PDFium can parse it. Tests use it instead of binary fixtures so the
// geometry under test is visible in the test itself.
func pdfWithPageSpecs(pages []pdfPage) []byte {
	// Object 1 is the catalog, 2 the page tree, 3 the font; everything else is
	// appended as it is built.
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"", // page tree, filled in once the kids are known
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	add := func(body string) int {
		objects = append(objects, body)
		return len(objects)
	}

	var pageRefs []string
	for _, page := range pages {
		var content strings.Builder
		xobjects := make([]string, 0, len(page.Images))
		for i, img := range page.Images {
			num := add(imageXObject())
			name := fmt.Sprintf("Im%d", i+1)
			xobjects = append(xobjects, fmt.Sprintf("/%s %d 0 R", name, num))
			fmt.Fprintf(&content, "q %g 0 0 %g %g %g cm /%s Do Q\n", img.W, img.H, img.X, img.Y, name)
		}
		for _, r := range page.Runs {
			size := r.Size
			if size == 0 {
				size = 12
			}
			fmt.Fprintf(&content, "BT /F1 %g Tf %g %g Td (%s) Tj ET\n",
				size, r.X, r.Y, escapePDFString(r.Text))
		}

		resources := "/Font << /F1 3 0 R >>"
		if len(xobjects) > 0 {
			resources += " /XObject << " + strings.Join(xobjects, " ") + " >>"
		}
		contentNum := len(objects) + 2 // page object first, then its content
		pageNum := add(fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %g %g] /Contents %d 0 R /Resources << %s >> >>",
			testPageWidth, testPageHeight, contentNum, resources))
		add(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", content.Len(), content.String()))
		pageRefs = append(pageRefs, fmt.Sprintf("%d 0 R", pageNum))
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

// imageXObject is a 2x2 uncompressed RGB image — the smallest thing PDFium
// still counts as a raster image object.
func imageXObject() string {
	pixels := "\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\x00"
	return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 2 "+
		"/ColorSpace /DeviceRGB /BitsPerComponent 8 /Length %d >>\nstream\n%s\nendstream",
		len(pixels), pixels)
}

// pdfWithPages builds a text-only multi-page PDF.
func pdfWithPages(pages [][]textRun) []byte {
	specs := make([]pdfPage, 0, len(pages))
	for _, runs := range pages {
		specs = append(specs, pdfPage{Runs: runs})
	}
	return pdfWithPageSpecs(specs)
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
