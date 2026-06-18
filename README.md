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

## Status

> Early scaffold. The API below is the target design; see the roadmap.

## Features

| Capability | Engine | Status |
|------------|--------|--------|
| PDF text + word/char bounding boxes | PDFium (WASM) | Planned (core) |
| Page → PNG/JPEG screenshot | PDFium (WASM) | Planned (core) |
| Pluggable OCR (scanned PDFs/images) | Tesseract CLI or HTTP | Planned |
| Office formats (DOCX/XLSX/PPTX) | excelite + LibreOffice headless | Roadmap |
| Batch parsing | — | Roadmap |

## Install

```bash
go get github.com/promptrails/parserails
```

No system dependencies. PDFium ships as a WASM module loaded at runtime via wazero.

## Usage (target API)

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

	for _, w := range doc.Words {
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
	FontSize       float64
}
```

## OCR (pluggable)

Scanned/image-only pages have no extractable text. ParseRails detects empty pages
and falls back to an OCR backend you provide:

```go
type OCR interface {
	Recognize(ctx context.Context, img image.Image) ([]Word, error)
}
```

Backends planned: a cgo-free `tesseract` CLI adapter (PNG → TSV with bboxes) and
an HTTP adapter for remote OCR servers (EasyOCR/PaddleOCR-style).

## Architecture

- **cgo-free by default** — PDFium runs as WASM under wazero. Opt into a native
  cgo build later if you need maximum throughput.
- **Pooled runtime** — WASM instances are reused across requests (PDF parsing is
  CPU-bound); one pool per process.
- **Layered** — `domain → parser → ocr → service`, mirroring the PromptRails
  service conventions so it drops cleanly into a standalone parse service.

## Roadmap

- [ ] PDFium WASM core: structured word/char extraction
- [ ] Page screenshot rendering (DPI / pixel size)
- [ ] OCR fallback for scanned pages (Tesseract CLI adapter)
- [ ] Office formats via LibreOffice headless + excelize
- [ ] CLI (`parserails parse file.pdf`)
- [ ] Batch parsing + concurrency controls

## License

[MIT](./LICENSE) © PromptRails
