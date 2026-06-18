package parserails

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIsOfficeFormat(t *testing.T) {
	cases := map[string]bool{
		"a.pdf": false, "a.PDF": false, "a.txt": false,
		"a.docx": true, "a.DOCX": true, "report.pptx": true,
		"sheet.xlsx": true, "old.doc": true, "x.odt": true,
	}
	for path, want := range cases {
		if got := IsOfficeFormat(path); got != want {
			t.Errorf("IsOfficeFormat(%q) = %v, want %v", path, got, want)
		}
	}
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseFile(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = p.Close() }()

	path := writeTemp(t, "doc.pdf", minimalPDF("Hello World"))
	doc, err := p.ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got := doc.Text(); got != "Hello World" {
		t.Fatalf("text = %q", got)
	}
}

func TestParseFilesBatch(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = p.Close() }()

	paths := []string{
		writeTemp(t, "a.pdf", minimalPDF("alpha")),
		writeTemp(t, "b.pdf", minimalPDF("beta")),
		filepath.Join(t.TempDir(), "missing.pdf"), // should error, not panic
	}
	results := p.ParseFiles(context.Background(), paths, 2)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].Err != nil || results[0].Document.Text() != "alpha" {
		t.Errorf("result 0: %+v", results[0])
	}
	if results[1].Err != nil || results[1].Document.Text() != "beta" {
		t.Errorf("result 1: %+v", results[1])
	}
	if results[2].Err == nil {
		t.Errorf("result 2 should have errored on a missing file")
	}
}
