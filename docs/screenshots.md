# Page Screenshots

ParseRails renders any page to an image — useful for LLM-vision pipelines,
thumbnails, or as the input to an [OCR](ocr.md) backend for scanned pages.

Rendering uses the same PDFium runtime as text extraction, so there is no extra
dependency and no cgo.

```go
img, err := p.RenderPage(ctx, pdf, parserails.RenderRequest{
	Page: 0,
	DPI:  150, // defaults to 150 when zero
})
if err != nil {
	panic(err)
}

f, _ := os.Create("page-0.png")
defer f.Close()
png.Encode(f, img)
```

`RenderPage` returns a standard `image.Image` (an `*image.RGBA` copied out of the
WASM buffer, so it stays valid after the call returns).

## Mapping boxes onto the image

Because text boxes and the rendered image come from the same engine, you can map
a `Word`'s PDF-space box onto pixel coordinates in the screenshot. At `DPI`, one
PDF point is `DPI/72` pixels, and the image's Y axis is flipped (top-left origin):

```go
scale := float64(dpi) / 72.0
for _, w := range doc.Pages[0].Words {
	px0 := w.X0 * scale
	px1 := w.X1 * scale
	py0 := (pageHeight - w.Y1) * scale // top edge
	py1 := (pageHeight - w.Y0) * scale // bottom edge
	_ = px0; _ = px1; _ = py0; _ = py1
}
```
