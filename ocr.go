package parserails

import (
	"context"
	"image"
)

// OCR is a pluggable optical-character-recognition backend.
//
// ParseRails calls an OCR backend as a fallback for pages that yield no
// extractable text (scanned or image-only PDFs): it renders the page to an
// image and hands it to Recognize.
//
// Coordinate contract: Recognize returns words in the **pixel space of the given
// image** — origin at the top-left, X to the right, Y downward, in pixels.
// ParseRails translates those boxes back into PDF user space (bottom-left
// origin, points) so OCR words are interchangeable with natively extracted ones.
// This keeps backends simple: they only deal with the image they were handed.
type OCR interface {
	// Recognize extracts words with pixel-space bounding boxes from a page image.
	Recognize(ctx context.Context, img image.Image) ([]Word, error)
}

// noOCR is the default backend: it recognizes nothing. Pages without extractable
// text simply come back empty unless a real OCR backend is configured.
type noOCR struct{}

func (noOCR) Recognize(context.Context, image.Image) ([]Word, error) { return nil, nil }
