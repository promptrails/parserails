package parserails

import (
	"context"
	"fmt"
	"image"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
)

// What counts as a figure worth reading: small logos and rules are not worth a
// round trip through an OCR engine, and reading them tends to inject letterhead
// noise into otherwise clean text.
const (
	minFigureCoverage = 0.02 // fraction of the page
	minFigureSide     = 24.0 // points
)

// ocrFigures recognizes the text inside a page's raster figures and merges it
// with the page's native words.
//
// A page whose text layer is empty is handled by the whole-page fallback. This
// is the other half: pages that do carry text but keep part of it inside
// images — a scanned table pasted into a report, a chart with labels, a
// letterhead. Only those figure regions are rendered and recognized, so the
// cost scales with the pictures, not with the page.
func (p *Parser) ocrFigures(ctx context.Context, inst pdfium.Pdfium, page requests.Page, size pageSize, native []Word, pageIndex int, regions []ImageRegion) ([]Word, error) {
	pageArea := size.Width * size.Height
	figures := make([]ImageRegion, 0, len(regions))
	for _, r := range regions {
		if r.X1-r.X0 < minFigureSide || r.Y1-r.Y0 < minFigureSide {
			continue
		}
		if pageArea > 0 && r.Area()/pageArea < minFigureCoverage {
			continue
		}
		figures = append(figures, r)
	}
	if len(figures) == 0 {
		return native, nil
	}

	img, ratio, cleanup, err := renderInstance(inst, page, defaultOCRDPI)
	if err != nil {
		return native, err
	}
	raster := cloneRGBA(img)
	cleanup()

	out := native
	for _, region := range figures {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		words, err := p.ocrRegion(ctx, raster, region, ratio, size.Height)
		if err != nil {
			return nil, err
		}
		for i := range words {
			words[i].Page = pageIndex
		}
		out = mergeWords(out, words)
	}
	return out, nil
}

// ocrRegion recognizes one figure out of an already-rendered page raster.
func (p *Parser) ocrRegion(ctx context.Context, raster *image.RGBA, region ImageRegion, ratio, pageHeight float64) ([]Word, error) {
	rect := pixelRect(region, ratio, pageHeight).Intersect(raster.Bounds())
	if rect.Dx() <= 1 || rect.Dy() <= 1 {
		return nil, nil
	}

	// SubImage keeps the parent's coordinates, but encoding it to PNG does
	// not: the backend sees an image whose origin is the crop's corner, so its
	// boxes come back relative to the crop and are shifted back here.
	crop := raster.SubImage(rect)
	words, err := p.ocr.Recognize(ctx, crop)
	if err != nil {
		return nil, fmt.Errorf("parserails: ocr figure: %w", err)
	}
	for i := range words {
		words[i].X0 += float64(rect.Min.X)
		words[i].X1 += float64(rect.Min.X)
		words[i].Y0 += float64(rect.Min.Y)
		words[i].Y1 += float64(rect.Min.Y)
	}
	return pixelsToPoints(words, ratio, pageHeight), nil
}

// pixelRect maps a region in PDF user space onto the rendered raster's pixel
// grid (top-left origin).
func pixelRect(r ImageRegion, ratio, pageHeight float64) image.Rectangle {
	if ratio <= 0 {
		return image.Rectangle{}
	}
	return image.Rect(
		int(r.X0/ratio),
		int((pageHeight-r.Y1)/ratio),
		int(r.X1/ratio+1),
		int((pageHeight-r.Y0)/ratio+1),
	)
}

// overlapAcceptance is how much of a recognized word may sit on top of native
// text before it is treated as a duplicate of it.
const overlapAcceptance = 0.5

// mergeWords adds recognized words to native ones, dropping those that land on
// top of text the document already spells out. Native text wins: it is exact,
// while OCR of the same glyphs is a guess with a worse box.
func mergeWords(native, recognized []Word) []Word {
	out := native
	for _, w := range recognized {
		if w.Text == "" || coveredBy(w, native) {
			continue
		}
		out = append(out, w)
	}
	return out
}

func coveredBy(w Word, others []Word) bool {
	area := (w.X1 - w.X0) * (w.Y1 - w.Y0)
	if area <= 0 {
		return false
	}
	var covered float64
	for _, o := range others {
		dx := min(w.X1, o.X1) - max(w.X0, o.X0)
		dy := min(w.Y1, o.Y1) - max(w.Y0, o.Y0)
		if dx > 0 && dy > 0 {
			covered += dx * dy
			if covered/area >= overlapAcceptance {
				return true
			}
		}
	}
	return false
}
