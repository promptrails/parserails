# Images as Input

A scan, a photo of a receipt, a screenshot — input that never was a PDF and has
no text layer at all. `ParseImage` reads one through the configured OCR
backend:

```go
p, _ := parserails.New(parserails.WithOCR(tesseract.New(tesseract.Config{})))

doc, err := p.ParseImage(ctx, jpegBytes)
fmt.Println(doc.Text())
```

Routing does it for you: `ParseData`, `ParseFile` and `ExtractTextData` send
anything that [sniffs](formats.md) as an image down this path.

```go
doc, _ := p.ParseFile(ctx, "receipt.jpg")
```

Decoders are pure Go, so the cgo-free promise holds: **PNG, JPEG, GIF** from the
standard library, **TIFF, BMP, WebP** from `golang.org/x/image`.

## Coordinates

There is no page here, so the "page" is the image: `Page.Width` and
`Page.Height` are its **pixel** dimensions. Word boxes keep those pixels but
flip to a bottom-left origin, so image words obey the same convention as PDF
words and the same downstream code reads both.

The decoder reads one frame. Animated GIFs and multi-page TIFFs are not expanded
into a page sequence. Image dimensions are checked before pixel allocation;
see [Limits & Timeouts](limits.md) for the current pixel cap.

## Without OCR

`ParseImage` needs an OCR backend and says so rather than returning an empty
document. [`Inspect`](complexity.md) on an image is a constant: one page,
`full_page_image`, `needs_ocr`, reason `scanned`.
