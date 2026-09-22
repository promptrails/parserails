package parserails

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestInspectClassifiesPages(t *testing.T) {
	p := newTestParser(t)

	pdf := pdfWithPageSpecs([]pdfPage{
		// A normal text page.
		{Runs: []textRun{
			{Text: "This page has a paragraph of perfectly readable text on it.", X: 72, Y: 700},
			{Text: "It continues for a second line so the page is not sparse.", X: 72, Y: 680},
		}},
		// A scan: one raster over the whole page, no text.
		{Images: []imageBox{{X: 0, Y: 0, W: testPageWidth, H: testPageHeight}}},
		// A blank page.
		{},
		// Text with a substantial figure beside it.
		{
			Runs:   []textRun{{Text: "Figure 1 shows the quarterly totals for each region.", X: 72, Y: 700}},
			Images: []imageBox{{X: 72, Y: 300, W: 400, H: 300}},
		},
	})

	c, err := p.Inspect(context.Background(), pdf, ReadOptions{})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(c.Pages) != 4 {
		t.Fatalf("got %d pages, want 4", len(c.Pages))
	}

	if got := c.Pages[0]; got.NeedsOCR {
		t.Errorf("text page flagged: %+v", got)
	}
	if got := c.Pages[1]; !got.NeedsOCR || !contains(got.Reasons, ReasonScanned) {
		t.Errorf("scanned page = %+v, want needs_ocr with reason %q", got, ReasonScanned)
	}
	if !c.Pages[1].FullPageImage {
		t.Errorf("full-page raster not detected: %+v", c.Pages[1])
	}
	if got := c.Pages[2]; !got.NeedsOCR || !contains(got.Reasons, ReasonNoText) {
		t.Errorf("blank page = %+v, want needs_ocr with reason %q", got, ReasonNoText)
	}
	if got := c.Pages[3]; !got.NeedsOCR || !contains(got.Reasons, ReasonEmbeddedImages) {
		t.Errorf("figure page = %+v, want needs_ocr with reason %q", got, ReasonEmbeddedImages)
	}

	if !c.NeedsOCR() {
		t.Error("document should need OCR")
	}
	if got := fmt.Sprint(c.OCRPages()); got != "[1 2 3]" {
		t.Errorf("OCRPages = %s, want [1 2 3]", got)
	}
}

func TestInspectHonoursPageSelection(t *testing.T) {
	p := newTestParser(t)
	pdf := pdfWithPages([][]textRun{
		{{Text: "Page one has enough text to count as a real page of prose.", X: 72, Y: 700}},
		{{Text: "Page two likewise carries a full sentence of readable text.", X: 72, Y: 700}},
	})

	c, err := p.Inspect(context.Background(), pdf, ReadOptions{Pages: "2"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(c.Pages) != 1 || c.Pages[0].Page != 1 {
		t.Fatalf("pages = %+v, want only page index 1", c.Pages)
	}
}

func TestIsGarbled(t *testing.T) {
	cases := map[string]bool{
		"A perfectly ordinary sentence of text.": false,
		"(cid:12)(cid:9)(cid:40)":                true,
		"����������������":                       true,
		"short":                 false, // too little text to judge
		strings.Repeat("", 40): true,
	}
	for text, want := range cases {
		if got := isGarbled(text); got != want {
			t.Errorf("isGarbled(%.20q) = %v, want %v", text, got, want)
		}
	}
}
