package parserails

import (
	"errors"
	"fmt"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	pdfium_errors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
)

// ImageRegion is an embedded raster image on a page, with its box in PDF user
// space (bottom-left origin, points).
type ImageRegion struct {
	// Index is the image's position among the page's image objects, so a
	// figure can be named stably across runs (img_p1_2).
	Index          int     `json:"index"`
	X0, Y0, X1, Y1 float64 `json:"-"`
}

// Area is the region's area in square points.
func (r ImageRegion) Area() float64 { return (r.X1 - r.X0) * (r.Y1 - r.Y0) }

// maxFormDepth bounds recursion into nested Form XObjects.
const maxFormDepth = 8

// pageImages lists the raster images drawn on a page, including those placed
// through a Form XObject.
//
// PDFium walks page objects one at a time, and each step is a call across the
// WASM boundary, so this is only worth doing where the answer is used: figure
// OCR, complexity inspection, Markdown figures.
func pageImages(inst pdfium.Pdfium, page requests.Page) ([]ImageRegion, error) {
	count, err := inst.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: page})
	if err != nil {
		if unsupported(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("parserails: count page objects: %w", err)
	}

	var out []ImageRegion
	for i := 0; i < count.Count; i++ {
		obj, err := inst.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: page, Index: i})
		if err != nil {
			return nil, fmt.Errorf("parserails: page object %d: %w", i, err)
		}
		if err := collectImages(inst, obj.PageObject, identityMatrix, &out, 0); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// collectImages walks one page object, descending into Form XObjects.
//
// A placed figure is usually a form, not a bare image: the page draws the
// form and the image sits inside it, in the form's own coordinates. Taking
// only direct image objects therefore misses most real figures, and the
// nested ones need the form's matrix applied to be anywhere near the page
// they are drawn on.
func collectImages(inst pdfium.Pdfium, obj references.FPDF_PAGEOBJECT, m matrix, out *[]ImageRegion, depth int) error {
	kind, err := inst.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj})
	if err != nil {
		return fmt.Errorf("parserails: page object type: %w", err)
	}

	switch kind.Type {
	case enums.FPDF_PAGEOBJ_IMAGE:
		bounds, err := inst.FPDFPageObj_GetBounds(&requests.FPDFPageObj_GetBounds{PageObject: obj})
		if err != nil {
			if unsupported(err) {
				return nil
			}
			return fmt.Errorf("parserails: page object bounds: %w", err)
		}
		box := m.apply(float64(bounds.Left), float64(bounds.Bottom),
			float64(bounds.Right), float64(bounds.Top))
		box.Index = len(*out)
		*out = append(*out, box)

	case enums.FPDF_PAGEOBJ_FORM:
		if depth >= maxFormDepth {
			return nil
		}
		count, err := inst.FPDFFormObj_CountObjects(&requests.FPDFFormObj_CountObjects{PageObject: obj})
		if err != nil {
			if unsupported(err) {
				return nil
			}
			return fmt.Errorf("parserails: count form objects: %w", err)
		}
		inner := m
		if got, err := inst.FPDFPageObj_GetMatrix(&requests.FPDFPageObj_GetMatrix{PageObject: obj}); err == nil {
			inner = m.compose(matrix{
				a: float64(got.Matrix.A), b: float64(got.Matrix.B),
				c: float64(got.Matrix.C), d: float64(got.Matrix.D),
				e: float64(got.Matrix.E), f: float64(got.Matrix.F),
			})
		} else if !unsupported(err) {
			return fmt.Errorf("parserails: form matrix: %w", err)
		}
		for i := 0; i < count.Count; i++ {
			child, err := inst.FPDFFormObj_GetObject(&requests.FPDFFormObj_GetObject{
				PageObject: obj, Index: uint64(i),
			})
			if err != nil {
				continue // one unreadable child is not a broken form
			}
			if err := collectImages(inst, child.PageObject, inner, out, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// matrix is a PDF transformation matrix [a b c d e f], mapping a nested
// object's coordinates into the page's.
type matrix struct{ a, b, c, d, e, f float64 }

var identityMatrix = matrix{a: 1, d: 1}

// compose returns the matrix that applies inner first, then m.
func (m matrix) compose(inner matrix) matrix {
	return matrix{
		a: inner.a*m.a + inner.b*m.c,
		b: inner.a*m.b + inner.b*m.d,
		c: inner.c*m.a + inner.d*m.c,
		d: inner.c*m.b + inner.d*m.d,
		e: inner.e*m.a + inner.f*m.c + m.e,
		f: inner.e*m.b + inner.f*m.d + m.f,
	}
}

// apply maps a box through the matrix, taking the bounds of the four
// transformed corners so a rotated placement still reports a box that
// contains it.
func (m matrix) apply(x0, y0, x1, y1 float64) ImageRegion {
	xs := [4]float64{
		m.a*x0 + m.c*y0 + m.e, m.a*x1 + m.c*y0 + m.e,
		m.a*x0 + m.c*y1 + m.e, m.a*x1 + m.c*y1 + m.e,
	}
	ys := [4]float64{
		m.b*x0 + m.d*y0 + m.f, m.b*x1 + m.d*y0 + m.f,
		m.b*x0 + m.d*y1 + m.f, m.b*x1 + m.d*y1 + m.f,
	}
	out := ImageRegion{X0: xs[0], Y0: ys[0], X1: xs[0], Y1: ys[0]}
	for i := 1; i < 4; i++ {
		out.X0, out.Y0 = min(out.X0, xs[i]), min(out.Y0, ys[i])
		out.X1, out.Y1 = max(out.X1, xs[i]), max(out.Y1, ys[i])
	}
	return out
}

// unsupported reports whether PDFium refused a call because this build does not
// have it, rather than because the document is broken. The native cgo backend
// omits PDFium's experimental API unless it was built with the
// pdfium_experimental tag; features that lean on it degrade instead of failing.
func unsupported(err error) bool {
	return errors.Is(err, pdfium_errors.ErrExperimentalUnsupported) ||
		errors.Is(err, pdfium_errors.ErrUnsupportedOnWebassembly)
}
