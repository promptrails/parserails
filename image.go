package parserails

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"  // register the GIF decoder
	_ "image/jpeg" // register the JPEG decoder
	_ "image/png"  // register the PNG decoder

	_ "golang.org/x/image/bmp"  // register the BMP decoder
	_ "golang.org/x/image/tiff" // register the TIFF decoder
	_ "golang.org/x/image/webp" // register the WebP decoder
)

// maxImagePixels caps how large a raster ParseRails will decode. Image headers
// are untrusted input and decoders allocate from them: a hundred-byte PNG can
// declare 60000x60000 and reserve fourteen gigabytes before failing on the
// pixel data that was never there.
const maxImagePixels = 100 << 20 // 100 megapixels — far past any real scan

// ParseImage reads a standalone raster image — a scan, a photo of a receipt, a
// screenshot — through the configured OCR backend.
//
// There is no PDF and no text layer here, so an OCR backend is required. The
// result is a one-page Document whose page is the image itself: coordinates are
// in **pixels**, with the origin flipped to the bottom-left so image words obey
// the same convention as PDF words.
func (p *Parser) ParseImage(ctx context.Context, data []byte) (*Document, error) {
	return bounded(ctx, p, func(ctx context.Context) (*Document, error) {
		return p.parseImage(ctx, data)
	})
}

func (p *Parser) parseImage(ctx context.Context, data []byte) (*Document, error) {
	if !p.hasOCR() {
		return nil, fmt.Errorf("parserails: reading an image needs an OCR backend (see WithOCR)")
	}
	// Read the header first and check the declared size against the budget,
	// before any decoder allocates a pixel buffer from it.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parserails: decode image header: %w", err)
	}
	if pixels := int64(cfg.Width) * int64(cfg.Height); pixels > maxImagePixels {
		return nil, fmt.Errorf("parserails: image declares %dx%d (%d megapixels), over the %d megapixel limit",
			cfg.Width, cfg.Height, pixels>>20, int64(maxImagePixels)>>20)
	}

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parserails: decode image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	words, err := p.ocr.Recognize(ctx, img)
	if err != nil {
		return nil, fmt.Errorf("parserails: ocr %s image: %w", format, err)
	}

	bounds := img.Bounds()
	width, height := float64(bounds.Dx()), float64(bounds.Dy())
	return &Document{Pages: []Page{{
		Index:  0,
		Width:  width,
		Height: height,
		// The backend reported pixel boxes with a top-left origin; a Document
		// is bottom-left everywhere, so flip without rescaling.
		Words: pixelsToPoints(words, 1, height),
	}}}, nil
}
