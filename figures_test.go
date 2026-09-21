package parserails

import (
	"context"
	"image"
	"testing"
)

// fakeOCR returns fixed words, in the pixel space of whatever image it is
// handed, and records the crops it saw.
type fakeOCR struct {
	words []Word
	crops []image.Rectangle
}

func (f *fakeOCR) Recognize(_ context.Context, img image.Image) ([]Word, error) {
	f.crops = append(f.crops, img.Bounds())
	out := make([]Word, len(f.words))
	copy(out, f.words)
	return out, nil
}

func TestImageOCRReadsFiguresAndPlacesThem(t *testing.T) {
	ocr := &fakeOCR{words: []Word{
		{Text: "CHART-LABEL", X0: 2, Y0: 2, X1: 40, Y1: 14, Confidence: 0.9},
	}}
	p := newTestParser(t, WithOCR(ocr), WithImageOCR())

	figure := imageBox{X: 72, Y: 300, W: 400, H: 300}
	pdf := pdfWithPageSpecs([]pdfPage{{
		Runs:   []textRun{{Text: "Native paragraph text", X: 72, Y: 700}},
		Images: []imageBox{figure},
	}})

	doc, err := p.Parse(context.Background(), pdf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var found *Word
	for i, w := range doc.Words() {
		if w.Text == "CHART-LABEL" {
			found = &doc.Words()[i]
		}
	}
	if found == nil {
		t.Fatalf("figure text missing from %+v", doc.Words())
	}
	// It must land inside the figure it was read from, in PDF points.
	if found.X0 < figure.X || found.X1 > figure.X+figure.W ||
		found.Y0 < figure.Y || found.Y1 > figure.Y+figure.H {
		t.Errorf("figure word placed outside its figure: %+v", *found)
	}
	if !found.IsOCR() {
		t.Error("figure word should carry its OCR confidence")
	}
	if len(ocr.crops) != 1 {
		t.Fatalf("OCR ran %d times, want once (one substantial figure)", len(ocr.crops))
	}
	if r := ocr.crops[0]; r.Dx() < 100 || r.Dy() < 100 {
		t.Errorf("crop = %v, want roughly the figure's pixels", r)
	}
}

func TestImageOCRIsOptIn(t *testing.T) {
	ocr := &fakeOCR{words: []Word{{Text: "CHART-LABEL", X0: 2, Y0: 2, X1: 40, Y1: 14}}}
	p := newTestParser(t, WithOCR(ocr)) // no WithImageOCR

	pdf := pdfWithPageSpecs([]pdfPage{{
		Runs:   []textRun{{Text: "Native paragraph text", X: 72, Y: 700}},
		Images: []imageBox{{X: 72, Y: 300, W: 400, H: 300}},
	}})
	doc, err := p.Parse(context.Background(), pdf)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(ocr.crops) != 0 {
		t.Fatalf("OCR ran without WithImageOCR")
	}
	if got := doc.Text(); got != "Native paragraph text" {
		t.Fatalf("text = %q, want only the native text", got)
	}
}

func TestImageOCRSkipsSmallDecorations(t *testing.T) {
	ocr := &fakeOCR{words: []Word{{Text: "LOGO", X0: 1, Y0: 1, X1: 5, Y1: 5}}}
	p := newTestParser(t, WithOCR(ocr), WithImageOCR())

	pdf := pdfWithPageSpecs([]pdfPage{{
		Runs:   []textRun{{Text: "Native paragraph text", X: 72, Y: 700}},
		Images: []imageBox{{X: 500, Y: 750, W: 18, H: 18}}, // a bullet or a logo
	}})
	if _, err := p.Parse(context.Background(), pdf); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(ocr.crops) != 0 {
		t.Fatalf("OCR ran on a %d-point decoration", 18)
	}
}

func TestMergeWordsDropsDuplicatesOfNativeText(t *testing.T) {
	native := []Word{{Text: "Total", X0: 100, Y0: 700, X1: 140, Y1: 712}}
	recognized := []Word{
		{Text: "Total", X0: 101, Y0: 701, X1: 139, Y1: 711}, // same glyphs, worse box
		{Text: "42", X0: 200, Y0: 700, X1: 220, Y1: 712},    // only in the picture
	}
	got := mergeWords(native, recognized)
	if len(got) != 2 {
		t.Fatalf("merged = %+v, want the native word plus the new one", got)
	}
	if got[1].Text != "42" {
		t.Errorf("kept %q, want the word that was not already in the text layer", got[1].Text)
	}
}

func TestPixelRectFlipsTheYAxis(t *testing.T) {
	// PDFium's ratio is pixels per point: 200 DPI is 200/72 = 2.78.
	ratio := 200.0 / 72.0
	r := pixelRect(ImageRegion{X0: 72, Y0: 300, X1: 472, Y1: 600}, ratio, 792)
	if r.Min.X != 200 { // 72pt = one inch = 200px at 200 DPI
		t.Errorf("left = %d, want 200", r.Min.X)
	}
	// The top of the region (Y1=600) is 192 points below the page top.
	if want := int(192 * ratio); r.Min.Y != want {
		t.Errorf("top = %d, want %d", r.Min.Y, want)
	}
	if r.Dy() <= 0 || r.Dx() <= 0 {
		t.Errorf("degenerate rect %v", r)
	}
}

func TestPixelsToPointsUsesPixelsPerPoint(t *testing.T) {
	// One inch in from the left and down from the top, at 200 DPI.
	ratio := 200.0 / 72.0
	got := pixelsToPoints([]Word{{X0: 200, Y0: 200, X1: 400, Y1: 240}}, ratio, 792)[0]
	if got.X0 != 72 {
		t.Errorf("X0 = %v, want 72pt", got.X0)
	}
	if got.Y1 != 792-72 {
		t.Errorf("Y1 = %v, want %v", got.Y1, 792-72.0)
	}
	if got.X1 != 144 {
		t.Errorf("X1 = %v, want 144pt", got.X1)
	}
}

// A word recognized in the middle of a rendered page must come back in the
// middle of the page, not several pages away.
func TestWholePageOCRLandsOnThePage(t *testing.T) {
	ocr := &fakeOCR{words: []Word{{Text: "CENTRE", X0: 840, Y0: 1090, X1: 860, Y1: 1110}}}
	p := newTestParser(t, WithOCR(ocr))

	doc, err := p.Parse(context.Background(), pdfWithPageSpecs([]pdfPage{{}}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Pages[0].Words) != 1 {
		t.Fatalf("words = %+v", doc.Pages[0].Words)
	}
	w := doc.Pages[0].Words[0]
	page := doc.Pages[0]
	if w.X0 < 0 || w.X1 > page.Width || w.Y0 < 0 || w.Y1 > page.Height {
		t.Fatalf("word %+v is outside the %.0fx%.0f page", w, page.Width, page.Height)
	}
	// The raster centre at 200 DPI (1700x2200) is the page centre.
	if diff := w.X0 - page.Width/2; diff > 12 || diff < -12 {
		t.Errorf("X0 = %.1f, want near the page centre %.1f", w.X0, page.Width/2)
	}
	if diff := w.Y0 - page.Height/2; diff > 12 || diff < -12 {
		t.Errorf("Y0 = %.1f, want near the page centre %.1f", w.Y0, page.Height/2)
	}
}
