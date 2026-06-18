# Page Screenshots

ParseRails can render any page to an image — useful for LLM-vision pipelines,
thumbnails, or as the input to an [OCR](ocr.md) backend for scanned pages.

> Rendering is on the [Roadmap](roadmap.md). The API below is the target design.

Rendering uses the same PDFium runtime as text extraction, so there is no extra
dependency and no cgo.

```go
img, _ := p.RenderPage(ctx, pdf, parserails.RenderRequest{
	Page: 0,
	DPI:  150, // or specify pixel Width/Height instead
})

f, _ := os.Create("page-0.png")
defer f.Close()
png.Encode(f, img)
```

Because text boxes and the rendered image come from the same engine at a known
DPI, you can map a `Word`'s PDF-space box directly onto pixel coordinates in the
screenshot.
