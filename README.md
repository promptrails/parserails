# ParseRails

Fast, light, **cgo-free** document parsing for Go. Spatial text extraction with
bounding boxes, page screenshots, and pluggable OCR — no cloud, no LLM required.

[![Go Reference](https://pkg.go.dev/badge/github.com/promptrails/parserails.svg)](https://pkg.go.dev/github.com/promptrails/parserails)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

ParseRails is the Go counterpart to [run-llama/liteparse](https://github.com/run-llama/liteparse).
It wraps the **same engine liteparse uses — Google's PDFium** — through
[`klippa-app/go-pdfium`](https://github.com/klippa-app/go-pdfium), running it in
**pure-Go WebAssembly (wazero)** mode. That means no `CGO_ENABLED=1`, no native
libraries to install in your Docker image, and clean cross-compilation.

## Why

Most Go PDF text extractors give you a flat string and lose layout. ParseRails
keeps **spatial structure** — every word with its bounding box, page, and font —
which is what downstream RAG, table reconstruction, and LLM-vision pipelines
actually need.

## Features

| Capability | Engine | Status |
|------------|--------|--------|
| PDF text + per-word bounding boxes | PDFium (WASM) | ✅ |
| Word / line granularity, opt-in font size | PDFium (WASM) | ✅ |
| Page screenshot rendering (`RenderPage`) | PDFium (WASM) | ✅ |
| Pluggable OCR + automatic fallback | interface | ✅ |
| Tesseract OCR adapter (cgo-free, subprocess) | `tesseract` CLI | ✅ |
| HTTP OCR adapter | — | ⏳ Planned |
| Office formats (DOCX/XLSX/PPTX) | excelize + LibreOffice headless | 🗺️ Roadmap |
| CLI · batch parsing | — | 🗺️ Roadmap |

## Install

```bash
go get github.com/promptrails/parserails
```

No system dependencies. PDFium ships as a WASM module loaded at runtime via wazero.

## Usage

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/promptrails/parserails"
)

func main() {
	p, err := parserails.New() // initializes a pooled PDFium WASM runtime
	if err != nil {
		panic(err)
	}
	defer p.Close()

	pdf, _ := os.ReadFile("invoice.pdf")

	doc, err := p.Parse(context.Background(), pdf)
	if err != nil {
		panic(err)
	}

	for _, w := range doc.Words() {
		fmt.Printf("p%d %q [%.1f %.1f %.1f %.1f]\n",
			w.Page, w.Text, w.X0, w.Y0, w.X1, w.Y1)
	}
}
```

### Word

```go
type Word struct {
	Text           string
	Page           int
	X0, Y0, X1, Y1 float64 // bounding box, PDF user-space coordinates
	FontSize       float64 // 0 unless New(WithFontInfo()) is used
}
```

### Granularity

Word-level boxes are precise but extract every character across the WASM
boundary. For per-line boxes at ~6× the speed:

```go
p, _ := parserails.New(parserails.WithGranularity(parserails.GranularityLine))
```

## OCR (pluggable)

Scanned/image-only pages have no extractable text. ParseRails renders those pages
and falls back to an OCR backend you provide — automatically:

```go
import "github.com/promptrails/parserails/ocr/tesseract"

p, _ := parserails.New(
	parserails.WithOCR(tesseract.New(tesseract.Config{Lang: "eng"})),
)
```

The bundled `ocr/tesseract` adapter shells out to the `tesseract` binary (no cgo,
no `libtesseract`). Implement the one-method `OCR` interface for any other engine
or a remote service. An HTTP adapter is on the roadmap.

## Screenshots

```go
img, _ := p.RenderPage(ctx, pdf, parserails.RenderRequest{Page: 0, DPI: 150})
png.Encode(out, img) // standard image.Image
```

## Architecture

- **cgo-free by default** — PDFium runs as WASM under wazero. Opt into a native
  cgo build later if you need maximum throughput.
- **Pooled runtime** — WASM instances are reused across requests (PDF parsing is
  CPU-bound); one pool per process.
- **Layered** — `domain → parser → ocr → service`, mirroring the PromptRails
  service conventions so it drops cleanly into a standalone parse service.

## Benchmarks

ParseRails is benchmarked against `ledongthuc/pdf`, `pdfcpu/pdfcpu`, and
`unidoc/unipdf` in [`benchmark/`](./benchmark) — a **separate Go module**, so
those (heavy, partly AGPL/commercial) dependencies never enter this module's
graph.

```bash
cd benchmark && go test -bench=. -benchmem ./...
```

ParseRails is the only one of the four that returns per-word **bounding boxes**;
the pure-Go readers are far faster but give flat text only. See
[`benchmark/README.md`](./benchmark/README.md) for numbers and the (important)
caveats on what each library actually measures.

## Roadmap

- [x] PDFium WASM core: structured word/char extraction with boxes
- [x] Word / line granularity, opt-in font size
- [x] Page screenshot rendering (`RenderPage`)
- [x] OCR fallback for scanned pages + cgo-free Tesseract adapter
- [ ] HTTP OCR adapter (remote EasyOCR/PaddleOCR-style servers)
- [ ] Office formats via LibreOffice headless + excelize
- [ ] CLI (`parserails parse file.pdf`)
- [ ] Batch parsing + concurrency controls

See [`docs/roadmap.md`](./docs/roadmap.md) for the full status table.

## License

[MIT](./LICENSE) © PromptRails
