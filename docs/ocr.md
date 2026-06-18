# OCR

Scanned or image-only PDFs contain no extractable text. For those pages,
ParseRails delegates to a pluggable OCR backend that you provide.

## The interface

```go
type OCR interface {
	// Recognize extracts words with bounding boxes from a rendered page image.
	Recognize(ctx context.Context, img image.Image) ([]Word, error)
}
```

A backend receives a rendered page image and returns words in the same
PDF user-space convention as the native text extractor (see
[Spatial Text Extraction](parsing.md)), so OCR and native results are
interchangeable downstream.

## Configuring a backend

By default no OCR is performed — pages without extractable text come back empty.
Plug a backend in at construction time:

```go
p, _ := parserails.New(
	parserails.WithOCR(myTesseractBackend),
)
```

## Planned adapters

| Adapter | Approach | cgo |
|---------|----------|-----|
| Tesseract CLI | shell out to `tesseract`, parse TSV with bounding boxes | no |
| HTTP | POST the image to a remote OCR server (EasyOCR / PaddleOCR-style) | no |

> The Tesseract CLI adapter keeps the cgo-free promise: it invokes the
> `tesseract` binary as a subprocess rather than linking `libtesseract`.

See the [Roadmap](roadmap.md) for status.
