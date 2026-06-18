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
| HTTP OCR adapter (remote OCR servers) | `net/http` | ✅ |
| Office formats (DOCX/PPTX/XLSX/...) | LibreOffice headless → PDF | ✅ |
| Concurrent batch parsing | — | ✅ |
| CLI (`go install`) | — | ✅ |
| Native cgo build mode (hot paths) | — | 🗺️ Considering |

## Install

As a library:

```bash
go get github.com/promptrails/parserails
```

As a command:

```bash
go install github.com/promptrails/parserails/cmd/parserails@latest

parserails parse  invoice.pdf          # extract text
parserails parse  --json report.docx   # JSON, office docs via LibreOffice
parserails render --dpi 150 doc.pdf    # render page 0 → doc-p0.png
```

No system dependencies for PDF. PDFium ships as a WASM module loaded at runtime
via wazero. Office formats need `libreoffice`/`soffice` on PATH; OCR needs the
`tesseract` binary (only when enabled).

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

## Text-only fast path

When you need just the text (RAG ingestion, search indexing) and not boxes, use
`ExtractText` — it uses PDFium's whole-page text API (one call per page) and is
several times cheaper than `Parse`:

```go
text, _ := p.ExtractText(ctx, pdf)        // plain string, no boxes
text, _ = p.ExtractFileText(ctx, "x.docx") // PDF or office doc
```

## Backends: WASM (default) vs. native cgo

Same API, two build-time backends:

```bash
go build .                       # PDFium as WASM — cgo-free, portable (default)
go build -tags parserails_cgo .  # PDFium native — much faster/lighter, needs libpdfium
```

The default is cgo-free and needs no system libraries. The `parserails_cgo`
backend links `libpdfium` for far higher throughput on controlled hosts (e.g. a
Dockerized worker). `parserails.Backend` reports which is active. See
[docs/architecture.md](./docs/architecture.md).

## Architecture

- **cgo-free by default** — PDFium runs as WASM under wazero; opt into the native
  `parserails_cgo` backend for maximum throughput.
- **Pooled runtime** — instances are reused across requests (PDF parsing is
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

## Files & batches

```go
doc, _ := p.ParseFile(ctx, "report.docx")           // PDF or office doc
results := p.ParseFiles(ctx, paths, 4)               // concurrent batch
```

`ParseFile` converts office documents to PDF via LibreOffice, then parses.
`ParseFiles` runs a bounded-concurrency batch and captures per-file errors.

## Status

The PDF core and the full liteparse-style feature set (spatial text, rendering,
OCR, office formats, CLI, batch) are implemented. See
[`docs/roadmap.md`](./docs/roadmap.md) for the detailed status and what's still
being considered (e.g. a native cgo build mode).

## License

[MIT](./LICENSE) © PromptRails
