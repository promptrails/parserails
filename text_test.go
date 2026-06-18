package parserails

import (
	"context"
	"strings"
	"testing"
)

func TestExtractText(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = p.Close() }()

	text, err := p.ExtractText(context.Background(), minimalPDF("Hello World"))
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "Hello World") {
		t.Fatalf("text = %q, want it to contain %q", text, "Hello World")
	}
}

func TestBackendDefault(t *testing.T) {
	if Backend != "wasm" {
		t.Fatalf("default Backend = %q, want %q", Backend, "wasm")
	}
}
