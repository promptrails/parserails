package parserails

import (
	"context"
	"image"
)

// OCR is a pluggable optical-character-recognition backend.
//
// ParseRails calls an OCR backend as a fallback for pages that yield no
// extractable text (scanned or image-only PDFs). Implementations should return
// words with bounding boxes in the same PDF user-space convention as the native
// text extractor; see the tesseract and http adapters under ocr/.
type OCR interface {
	// Recognize extracts words with bounding boxes from a rendered page image.
	Recognize(ctx context.Context, img image.Image) ([]Word, error)
}

// noOCR is the default backend: it recognizes nothing. Pages without extractable
// text simply come back empty unless a real OCR backend is configured.
type noOCR struct{}

func (noOCR) Recognize(context.Context, image.Image) ([]Word, error) { return nil, nil }
