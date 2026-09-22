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

## The HTTP backend

`ocr/httpocr` delegates to a remote OCR server. It speaks the **LiteParse OCR
API** by default, so the OCR server images published for LiteParse — EasyOCR,
PaddleOCR, RapidOCR, Surya — work with ParseRails unchanged.

```go
import "github.com/promptrails/parserails/ocr/httpocr"

p, _ := parserails.New(parserails.WithOCR(httpocr.New(httpocr.Config{
	URL:      "http://localhost:8080/ocr",
	Language: "tur", // sent as the `language` field; defaults to "en"
	Header:   http.Header{"Authorization": {"Bearer " + token}},
})))
```

Server contract (`ProtocolOCRAPI`, the default):

```
POST <URL>   multipart/form-data: file=<page.png>, language=<code>
200 OK       {"results":[{"text":"hi","bbox":[x1,y1,x2,y2],"confidence":0.95}]}
```

`bbox` is `[x1, y1, x2, y2]` in image pixels with a top-left origin, and
`confidence` is 0–1, landing in `Word.Confidence`. A result that carries only a
detection `polygon` (rotated or vertical text) is reduced to its axis-aligned
bounds.

ParseRails' original protocol is still available for servers written against
it:

```go
httpocr.New(httpocr.Config{URL: url, Protocol: httpocr.ProtocolWords})
```

```
POST <URL>   body: image/png
200 OK       {"words":[{"text":"hi","x0":1,"y0":2,"x1":3,"y1":4,"confidence":0.9}]}
```

Responses are decoded leniently: whichever of `results` or `words` the server
sends is accepted, whatever protocol was configured.

## Figures on text pages

By default OCR is a **fallback**: it runs only on pages with no extractable
text at all. That misses the common middle case — a report whose text layer is
fine but whose numbers live inside a pasted screenshot or a chart.

`WithImageOCR` reads those too:

```go
p, _ := parserails.New(
	parserails.WithOCR(tesseract.New(tesseract.Config{})),
	parserails.WithImageOCR(),
)
```

On a page that already has text, every raster figure larger than 24 points a
side and 2% of the page is rendered and recognized, and the result is **merged**
with the native words. Recognized words that land on top of text the document
already spells out are dropped — native text is exact, OCR of the same glyphs
is a guess with a worse box.

The cost scales with the pictures, not the page count: one page render plus one
OCR call per substantial figure. It is off by default for that reason.

Pair it with [`Inspect`](complexity.md), which tells you up front which pages
carry figures worth reading (`embedded-images`).

## Routing and fast-path differences

`Inspect` reports OCR candidates but does not run OCR or change the fallback
trigger. Sparse or garbled text can be flagged while still containing native
words. Automatic whole-page fallback remains limited to pages with zero words.
If your policy requires recognizing the entire flagged page, render it and
invoke your backend explicitly.

PDF `ExtractText` / `ExtractTextData` use the text layer without OCR, even when
`WithOCR` is configured. Use `Parse` / `ParseData` when PDF OCR is required.
`ExtractTextData` on a standalone image does route through image OCR.

The HTTP adapter's default client timeout is 60 seconds; use `Config.Client`
to supply another client. The CLI defaults `--lang` to `eng` for both backends,
whereas a bare `httpocr.Config` defaults to `en`. Pass a language code supported
by your server. See [Limits & Timeouts](limits.md) for cancellation behavior.

## Writing your own

Anything that turns an image into positioned words works — a different engine, a
cloud API, a local model. Implement `Recognize`, return pixel-space boxes, and
pass it to `WithOCR`.
