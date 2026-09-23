package parserails

import (
	"context"
	"os"
	"sync"
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

// Building a Parser starts a PDFium WebAssembly runtime, which costs a second
// or two normally and far more under -race. The suite would spend most of its
// time doing that once per test, so the configurations nearly every test wants
// are built once and shared; a test that needs its own options still gets its
// own parser.
var (
	sharedParsers   = map[string]*Parser{}
	sharedParsersMu sync.Mutex
)

// newTestParser returns a Parser with the given options, shared across tests
// when the options are stateless.
func newTestParser(t *testing.T, opts ...Option) *Parser {
	t.Helper()
	if key, ok := sharedParserKey(opts); ok {
		return sharedParser(t, key, opts)
	}
	p, err := New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// sharedParserKey names a configuration that can be reused. Only the options
// tests use as plain settings qualify: anything carrying state of its own — an
// OCR backend that records what it saw — must not be shared.
func sharedParserKey(opts []Option) (string, bool) {
	switch len(opts) {
	case 0:
		return "default", true
	case 1:
		if sameOption(opts[0], WithFontInfo()) {
			return "font", true
		}
		if sameOption(opts[0], WithImages()) {
			return "images", true
		}
	case 2:
		if sameOption(opts[0], WithImages()) && sameOption(opts[1], WithFontInfo()) {
			return "images+font", true
		}
	}
	return "", false
}

// sameOption reports whether two options set the same field, by applying both
// to a zero config and comparing the result.
func sameOption(a, b Option) bool {
	var ca, cb config
	a(&ca)
	b(&cb)
	return ca.fontInfo == cb.fontInfo && ca.images == cb.images &&
		ca.granularity == cb.granularity && ca.ocr == nil && cb.ocr == nil &&
		ca.password == cb.password && ca.timeout == cb.timeout &&
		ca.nativeOffice == cb.nativeOffice && ca.imageOCR == cb.imageOCR
}

func sharedParser(t *testing.T, key string, opts []Option) *Parser {
	t.Helper()
	sharedParsersMu.Lock()
	defer sharedParsersMu.Unlock()
	if p, ok := sharedParsers[key]; ok {
		return p
	}
	p, err := New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sharedParsers[key] = p
	return p
}

// TestMain closes the shared parsers once the suite is done.
func TestMain(m *testing.M) {
	code := m.Run()
	for _, p := range sharedParsers {
		_ = p.Close()
	}
	os.Exit(code)
}
