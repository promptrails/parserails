package parserails

import (
	"context"
	"testing"
)

func TestRenderPage(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = p.Close() }()

	img, err := p.RenderPage(context.Background(), minimalPDF("Hello World"), RenderRequest{Page: 0, DPI: 100})
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	b := img.Bounds()
	// A 612×792pt page at 100 DPI ≈ 850×1100px. Just assert it's a real raster.
	if b.Dx() < 800 || b.Dy() < 1000 {
		t.Fatalf("unexpected image size: %dx%d", b.Dx(), b.Dy())
	}
}

func TestParseLineGranularity(t *testing.T) {
	p, err := New(WithGranularity(GranularityLine))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = p.Close() }()

	doc, err := p.Parse(context.Background(), minimalPDF("Hello World"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// One line of text → at least one rect covering "Hello World".
	if got := doc.Text(); got == "" {
		t.Fatal("line granularity returned no text")
	}
	for _, w := range doc.Words() {
		if w.X1 <= w.X0 || w.Y1 <= w.Y0 {
			t.Errorf("degenerate box: %+v", w)
		}
	}
}
