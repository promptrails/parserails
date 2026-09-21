package parserails

import (
	"errors"
	"fmt"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	pdfium_errors "github.com/klippa-app/go-pdfium/errors"
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

// pageImages lists the raster images drawn on a page.
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
		kind, err := inst.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj.PageObject})
		if err != nil {
			return nil, fmt.Errorf("parserails: page object %d type: %w", i, err)
		}
		if kind.Type != enums.FPDF_PAGEOBJ_IMAGE {
			continue
		}
		bounds, err := inst.FPDFPageObj_GetBounds(&requests.FPDFPageObj_GetBounds{PageObject: obj.PageObject})
		if err != nil {
			if unsupported(err) {
				return nil, nil // nothing useful to report on this backend
			}
			return nil, fmt.Errorf("parserails: page object %d bounds: %w", i, err)
		}
		out = append(out, ImageRegion{
			Index: len(out),
			X0:    float64(bounds.Left), Y0: float64(bounds.Bottom),
			X1: float64(bounds.Right), Y1: float64(bounds.Top),
		})
	}
	return out, nil
}

// unsupported reports whether PDFium refused a call because this build does not
// have it, rather than because the document is broken. The native cgo backend
// omits PDFium's experimental API unless it was built with the
// pdfium_experimental tag; features that lean on it degrade instead of failing.
func unsupported(err error) bool {
	return errors.Is(err, pdfium_errors.ErrExperimentalUnsupported) ||
		errors.Is(err, pdfium_errors.ErrUnsupportedOnWebassembly)
}
