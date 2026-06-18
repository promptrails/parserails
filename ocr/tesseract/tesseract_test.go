package tesseract

import (
	"context"
	"image"
	"image/color"
	"os/exec"
	"testing"
)

func TestParseTSV(t *testing.T) {
	b := &backend{cfg: Config{MinConfidence: 50}}
	// header + two word rows (level 5) + one low-confidence row that must drop.
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"5\t1\t1\t1\t1\t1\t10\t20\t40\t12\t96\tHello\n" +
		"5\t1\t1\t1\t1\t2\t55\t20\t50\t12\t91\tWorld\n" +
		"5\t1\t1\t1\t1\t3\t110\t20\t30\t12\t12\tnoise\n" +
		"4\t1\t1\t1\t1\t0\t0\t0\t0\t0\t-1\t\n"

	words, err := b.parseTSV([]byte(tsv))
	if err != nil {
		t.Fatalf("parseTSV: %v", err)
	}
	if len(words) != 2 {
		t.Fatalf("got %d words, want 2: %+v", len(words), words)
	}
	if words[0].Text != "Hello" || words[1].Text != "World" {
		t.Fatalf("texts = %q, %q", words[0].Text, words[1].Text)
	}
	// pixel space, top-left origin: left=10,top=20,w=40,h=12.
	if w := words[0]; w.X0 != 10 || w.Y0 != 20 || w.X1 != 50 || w.Y1 != 32 {
		t.Errorf("box = %+v, want [10 20 50 32]", w)
	}
}

func TestDefaults(t *testing.T) {
	b := New(Config{}).(*backend)
	if b.cfg.Binary != "tesseract" || b.cfg.Lang != "eng" || b.cfg.PSM != 3 {
		t.Fatalf("defaults not applied: %+v", b.cfg)
	}
}

// TestRecognizeIntegration runs the real binary when it is installed.
func TestRecognizeIntegration(t *testing.T) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skip("tesseract binary not installed")
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for i := range img.Pix {
		img.Pix[i] = 255 // white canvas; no text, just exercise the pipeline
	}
	img.Set(0, 0, color.Black)

	if _, err := New(Config{}).Recognize(context.Background(), img); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
}
