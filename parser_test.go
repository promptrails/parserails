package parserails

import (
	"context"
	"fmt"
	"strings"
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

// minimalPDF builds a valid single-page PDF rendering text at (100,700),
// computing real xref offsets so PDFium can parse it.
func minimalPDF(text string) []byte {
	content := fmt.Sprintf("BT /F1 24 Tf 100 700 Td (%s) Tj ET\n", text)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

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
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", len(objects)+1, xref)

	return []byte(b.String())
}
