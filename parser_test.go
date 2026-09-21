package parserails

import (
	"context"
	"testing"
)

func TestParseExtractsWordsWithBoxes(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = p.Close() }()

	doc, err := p.Parse(context.Background(), minimalPDF("Hello World"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(doc.Pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(doc.Pages))
	}
	if got := doc.Text(); got != "Hello World" {
		t.Fatalf("text = %q, want %q", got, "Hello World")
	}

	words := doc.Words()
	if len(words) != 2 {
		t.Fatalf("got %d words, want 2: %+v", len(words), words)
	}
	for _, w := range words {
		if w.X1 <= w.X0 || w.Y1 <= w.Y0 {
			t.Errorf("word %q has a degenerate box: %+v", w.Text, w)
		}
		if w.Page != 0 {
			t.Errorf("word %q on page %d, want 0", w.Text, w.Page)
		}
	}
	// The text was drawn at y=700 on a 792-high page; the box should sit there.
	if words[0].Y0 < 600 {
		t.Errorf("unexpected vertical position for %q: %+v", words[0].Text, words[0])
	}
}
