package parserails

import (
	"context"
	"testing"
)

func TestPageImagesFindsFiguresInsideFormXObjects(t *testing.T) {
	p := newTestParser(t, WithImages())
	// A figure placed the way a real document places one: a Form XObject
	// drawn with a matrix, the image sitting inside it in unit coordinates.
	doc, err := p.Parse(context.Background(), pdfWithPageSpecs([]pdfPage{{
		Runs:       []textRun{{Text: "Figure 1 below", X: 72, Y: 700}},
		FormImages: []imageBox{{X: 72, Y: 300, W: 400, H: 300}},
	}}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	images := doc.Pages[0].Images
	if len(images) != 1 {
		t.Fatalf("found %d images, want the one inside the form", len(images))
	}
	// The form's matrix has to be applied, or the image reports the unit
	// square it occupies in the form's own coordinates.
	got := images[0]
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"X0", got.X0, 72}, {"Y0", got.Y0, 300},
		{"X1", got.X1, 472}, {"Y1", got.Y1, 600},
	} {
		if diff := c.got - c.want; diff > 1 || diff < -1 {
			t.Errorf("%s = %.1f, want %.0f", c.name, c.got, c.want)
		}
	}
}

func TestMatrixComposeAppliesInnerFirst(t *testing.T) {
	// Place a unit square with one matrix, inside a form placed with another.
	outer := matrix{a: 2, d: 2, e: 10, f: 20}
	inner := matrix{a: 3, d: 3, e: 1, f: 1}
	got := outer.compose(inner).apply(0, 0, 1, 1)

	// inner: (0,0)-(1,1) → (1,1)-(4,4); outer: → (12,22)-(18,28)
	if got.X0 != 12 || got.Y0 != 22 || got.X1 != 18 || got.Y1 != 28 {
		t.Fatalf("box = %+v, want [12 22 18 28]", got)
	}
}

func TestMatrixApplyBoundsARotation(t *testing.T) {
	// A quarter turn: the box that contains the rotated square.
	rotated := matrix{a: 0, b: 1, c: -1, d: 0}
	got := rotated.apply(0, 0, 2, 4)
	if got.X0 != -4 || got.Y0 != 0 || got.X1 != 0 || got.Y1 != 2 {
		t.Fatalf("box = %+v, want [-4 0 0 2]", got)
	}
}
