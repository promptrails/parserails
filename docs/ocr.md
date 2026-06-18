# OCR

Scanned or image-only PDFs contain no extractable text. For those pages,
ParseRails renders the page and delegates to a pluggable OCR backend. The
fallback is automatic: when a page yields zero words, the configured backend runs
on a rasterized copy of it.

## The interface

```go
type OCR interface {
	// Recognize extracts words with pixel-space bounding boxes from a page image.
	Recognize(ctx context.Context, img image.Image) ([]Word, error)
}
```

**Coordinate contract:** a backend returns boxes in the **pixel space of the
image it was handed** — top-left origin, pixels. ParseRails translates them back
into PDF user space (bottom-left origin, points) so OCR words are interchangeable
with natively extracted ones. Backends only deal with the image; ParseRails owns
the coordinate math.

## Configuring a backend

By default no OCR is performed — pages without extractable text come back empty.
Plug a backend in at construction time:

```go
import "github.com/promptrails/parserails/ocr/tesseract"

p, _ := parserails.New(
	parserails.WithOCR(tesseract.New(tesseract.Config{Lang: "eng"})),
)
```

Now `Parse` automatically OCRs any text-less page; the words it returns carry
real PDF-space bounding boxes.

## The Tesseract backend

`ocr/tesseract` shells out to the `tesseract` command-line binary and parses its
TSV output. It does **not** link `libtesseract`, so it keeps ParseRails' cgo-free
promise — it only requires the `tesseract` binary on `PATH`.

```go
tesseract.New(tesseract.Config{
	Binary:        "tesseract", // default
	Lang:          "eng",       // -l
	PSM:           3,           // --psm (page segmentation mode)
	MinConfidence: 60,          // drop words below this confidence (0–100)
})
```

## Writing your own

Anything that turns an image into positioned words works — a remote OCR service,
a different engine, a cloud API. Implement `Recognize`, return pixel-space boxes,
and pass it to `WithOCR`. An HTTP adapter for remote OCR servers is on the
[Roadmap](roadmap.md).
