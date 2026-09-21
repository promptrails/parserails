package parserails

import (
	"context"
	"testing"
)

func TestGroupLinesSplitsRowsAndOrdersColumns(t *testing.T) {
	// Deliberately out of reading order: the sweep must sort it back.
	words := []Word{
		{Text: "world", X0: 60, Y0: 700, X1: 100, Y1: 712},
		{Text: "second", X0: 10, Y0: 680, X1: 60, Y1: 692},
		{Text: "Hello", X0: 10, Y0: 700, X1: 50, Y1: 712},
		{Text: "line", X0: 65, Y0: 681, X1: 90, Y1: 691},
	}
	lines := groupLines(words)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	if got := lines[0].Text(); got != "Hello world" {
		t.Errorf("first line = %q, want %q", got, "Hello world")
	}
	if got := lines[1].Text(); got != "second line" {
		t.Errorf("second line = %q, want %q", got, "second line")
	}
	if lines[0].X0 != 10 || lines[0].X1 != 100 {
		t.Errorf("line box = [%v %v], want the union of its words", lines[0].X0, lines[0].X1)
	}
}

func TestGroupLinesKeepsSuperscriptsOnTheirLine(t *testing.T) {
	words := []Word{
		{Text: "cost", X0: 10, Y0: 700, X1: 40, Y1: 712},
		{Text: "1", X0: 41, Y0: 707, X1: 45, Y1: 714}, // superscript footnote marker
		{Text: "below", X0: 10, Y0: 670, X1: 50, Y1: 682},
	}
	lines := groupLines(words)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	if got := lines[0].Text(); got != "cost 1" {
		t.Errorf("first line = %q, want %q", got, "cost 1")
	}
}

func TestDocumentTextKeepsLineBreaksAndPageBreaks(t *testing.T) {
	p := newTestParser(t)

	doc, err := p.Parse(context.Background(), pdfWithPages([][]textRun{
		{
			{Text: "First line", X: 72, Y: 700, Size: 12},
			{Text: "Second line", X: 72, Y: 680, Size: 12},
		},
		{{Text: "Page two", X: 72, Y: 700, Size: 12}},
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := "First line\nSecond line\fPage two"
	if got := doc.Text(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got := len(doc.Lines()); got != 3 {
		t.Fatalf("got %d lines, want 3", got)
	}
}

// newTestParser builds a Parser and closes it when the test ends.
func newTestParser(t *testing.T, opts ...Option) *Parser {
	t.Helper()
	p, err := New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}
