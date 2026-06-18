package parserails

import (
	"testing"

	"github.com/klippa-app/go-pdfium/responses"
)

func ch(text string, l, t, r, b float64) *responses.GetPageTextStructuredChar {
	return &responses.GetPageTextStructuredChar{
		Text:          text,
		PointPosition: responses.CharPosition{Left: l, Top: t, Right: r, Bottom: b},
	}
}

func TestWordsFromChars(t *testing.T) {
	// "Hi" then a space then "Go" — two words, boxes are the char unions.
	chars := []*responses.GetPageTextStructuredChar{
		ch("H", 10, 20, 18, 8),
		ch("i", 18, 20, 24, 8),
		ch(" ", 24, 20, 28, 8),
		ch("G", 28, 22, 36, 6),
		ch("o", 36, 22, 44, 6),
	}
	words := wordsFromChars(chars, 0)
	if len(words) != 2 {
		t.Fatalf("got %d words, want 2: %+v", len(words), words)
	}
	if words[0].Text != "Hi" || words[1].Text != "Go" {
		t.Fatalf("texts = %q, %q", words[0].Text, words[1].Text)
	}
	// First word box = union of H and i: X0=10, X1=24, Y0=8 (bottom), Y1=20 (top).
	w := words[0]
	if w.X0 != 10 || w.X1 != 24 || w.Y0 != 8 || w.Y1 != 20 {
		t.Errorf("Hi box = %+v, want [10 8 24 20]", w)
	}
}

func TestWordsFromCharsTrailingWord(t *testing.T) {
	// No trailing space — the last word must still flush.
	chars := []*responses.GetPageTextStructuredChar{ch("a", 0, 1, 1, 0)}
	if got := wordsFromChars(chars, 3); len(got) != 1 || got[0].Text != "a" || got[0].Page != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestWordsFromRectsSkipsBlank(t *testing.T) {
	rects := []*responses.GetPageTextStructuredRect{
		{Text: "line one", PointPosition: responses.CharPosition{Left: 1, Top: 10, Right: 50, Bottom: 2}},
		{Text: "  ", PointPosition: responses.CharPosition{Left: 0, Top: 0, Right: 0, Bottom: 0}},
	}
	words := wordsFromRects(rects, 1)
	if len(words) != 1 || words[0].Text != "line one" || words[0].Page != 1 {
		t.Fatalf("got %+v", words)
	}
	if w := words[0]; w.X0 != 1 || w.X1 != 50 || w.Y0 != 2 || w.Y1 != 10 {
		t.Errorf("box = %+v, want [1 2 50 10]", w)
	}
}

func TestPixelsToPoints(t *testing.T) {
	// A 100pt-tall page rendered at ratio 0.5 pt/px (i.e. 200px tall).
	// A pixel box at top-left (x:20..40, y:10..30) → PDF points, Y flipped.
	in := []Word{{Text: "x", X0: 20, X1: 40, Y0: 10, Y1: 30}}
	out := pixelsToPoints(in, 0.5, 100)
	w := out[0]
	if w.X0 != 10 || w.X1 != 20 {
		t.Errorf("X = [%v %v], want [10 20]", w.X0, w.X1)
	}
	// top pixel 10 → Y1 = 100 - 5 = 95; bottom pixel 30 → Y0 = 100 - 15 = 85.
	if w.Y1 != 95 || w.Y0 != 85 {
		t.Errorf("Y = [%v %v], want [85 95]", w.Y0, w.Y1)
	}
}
