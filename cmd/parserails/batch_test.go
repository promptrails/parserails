package main

import (
	"path/filepath"
	"testing"
)

func TestPlanOutputsAreUnique(t *testing.T) {
	// report.pdf and report.docx collide on the stem; report.pdf.docx then
	// collides with the name that resolved the first collision.
	inputs := []string{
		filepath.Join("in", "report.pdf"),
		filepath.Join("in", "report.docx"),
		filepath.Join("in", "report.pdf.docx"),
		filepath.Join("in", "sub", "report.pdf"), // a different directory is fine
	}
	outputs := planOutputs(inputs, "in", "out", ".txt")

	if len(outputs) != len(inputs) {
		t.Fatalf("planned %d outputs for %d inputs", len(outputs), len(inputs))
	}
	seen := map[string]string{}
	for in, out := range outputs {
		if other, clash := seen[out]; clash {
			t.Errorf("%s and %s both write %s", other, in, out)
		}
		seen[out] = in
	}
	// The uncontested name in a subdirectory keeps the simple form.
	if got := outputs[filepath.Join("in", "sub", "report.pdf")]; got != filepath.Join("out", "sub", "report.txt") {
		t.Errorf("subdirectory output = %s", got)
	}
}
