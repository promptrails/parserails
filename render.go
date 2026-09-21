package parserails

import (
	"context"
	"fmt"
	"image"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
)

// defaultOCRDPI is the resolution at which pages are rasterized before OCR.
// 200 DPI is a common sweet spot for Tesseract accuracy vs. speed.
const defaultOCRDPI = 200

// RenderRequest describes how to rasterize a single page.
type RenderRequest struct {
	Page     int    // 0-based page index.
	DPI      int    // Render resolution; defaults to 150 when zero.
	Password string // Password for encrypted documents; defaults to WithPassword.
}

// RenderPage rasterizes a single page of the given PDF to an image. It uses the
// same PDFium runtime as text extraction, so there is no extra dependency and no
// cgo. Useful for thumbnails, LLM-vision input, or feeding an OCR backend.
func (p *Parser) RenderPage(ctx context.Context, data []byte, req RenderRequest) (image.Image, error) {
	return bounded(ctx, p, func(ctx context.Context) (image.Image, error) {
		return p.renderPage(ctx, data, req)
	})
}

func (p *Parser) renderPage(ctx context.Context, data []byte, req RenderRequest) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	inst, err := p.pool.GetInstance(p.acquireTimeout())
	if err != nil {
		return nil, fmt.Errorf("parserails: acquire instance: %w", err)
	}
	defer func() { _ = inst.Close() }()

	password := req.Password
	if password == "" {
		password = p.password
	}
	doc, err := openDocument(inst, data, password)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	}()

	pageReq := requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: req.Page}}
	dpi := req.DPI
	if dpi <= 0 {
		dpi = 150
	}
	img, _, cleanup, err := renderInstance(inst, pageReq, dpi)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Copy out of the WASM-backed buffer before it is released by cleanup.
	return cloneRGBA(img), nil
}

// renderInstance renders a page on an already-acquired instance. The returned
// cleanup MUST be called once the image is no longer needed (it frees the
// WASM-side buffer). ratio is PDFium's PointToPixelRatio: pixels per point.
func renderInstance(inst pdfium.Pdfium, page requests.Page, dpi int) (img *image.RGBA, ratio float64, cleanup func(), err error) {
	res, err := inst.RenderPageInDPI(&requests.RenderPageInDPI{Page: page, DPI: dpi})
	if err != nil {
		return nil, 0, nil, fmt.Errorf("parserails: render page: %w", err)
	}
	// RenderedImage, not the deprecated Image field. It is declared as
	// image.Image because the concrete type follows the requested format, so
	// this asserts rather than assuming — and releases the WASM-side buffer
	// before returning, since a caller that gets an error gets no cleanup func.
	img, ok := res.Result.RenderedImage.(*image.RGBA)
	if !ok {
		res.Cleanup()
		return nil, 0, nil, fmt.Errorf("parserails: render page: got %T, want *image.RGBA", res.Result.RenderedImage)
	}
	return img, res.Result.PointToPixelRatio, res.Cleanup, nil
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// ocrPage rasterizes a page and runs the configured OCR backend over it,
// translating the backend's pixel-space boxes into PDF user space.
func (p *Parser) ocrPage(ctx context.Context, inst pdfium.Pdfium, page requests.Page, size pageSize) ([]Word, error) {
	if _, ok := p.ocr.(noOCR); ok {
		return nil, nil // no backend configured; keep the page empty.
	}

	img, ratio, cleanup, err := renderInstance(inst, page, defaultOCRDPI)
	if err != nil {
		return nil, err
	}
	pix := cloneRGBA(img)
	cleanup()

	words, err := p.ocr.Recognize(ctx, pix)
	if err != nil {
		return nil, fmt.Errorf("parserails: ocr: %w", err)
	}
	return pixelsToPoints(words, ratio, size.Height), nil
}

// pixelsToPoints converts top-left pixel-space boxes into bottom-left PDF
// points.
//
// ratio is PDFium's PointToPixelRatio, which despite its name (and go-pdfium's
// comment) is dpi/72 — **pixels per point**, as its own definition shows:
// (widthInPoints * dpi/72) / widthInPoints. Pixels are therefore divided by it,
// not multiplied: at 200 DPI a 612×792pt page renders to 1700×2200px with
// ratio 2.78, and multiplying would put every OCR word several pages away from
// where it was read.
func pixelsToPoints(words []Word, ratio, pageHeightPts float64) []Word {
	if ratio <= 0 {
		return words
	}
	for i := range words {
		w := &words[i]
		left, right := w.X0/ratio, w.X1/ratio
		topPx, bottomPx := w.Y0, w.Y1
		w.X0, w.X1 = left, right
		w.Y1 = pageHeightPts - topPx/ratio    // top edge → higher Y
		w.Y0 = pageHeightPts - bottomPx/ratio // bottom edge → lower Y
		w.FontSize /= ratio
	}
	return words
}

// pageSize carries page dimensions in points for coordinate conversion.
type pageSize struct{ Width, Height float64 }
