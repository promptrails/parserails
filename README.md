# ParseRails

Fast, light, **cgo-free** document parsing for Go. Spatial text with bounding
boxes, Markdown reconstruction, page screenshots, OCR routing and recursive
extraction of files inside files — no cloud, no LLM required.

[![Go Reference](https://pkg.go.dev/badge/github.com/promptrails/parserails.svg)](https://pkg.go.dev/github.com/promptrails/parserails)
[![CI](https://github.com/promptrails/parserails/actions/workflows/ci.yml/badge.svg)](https://github.com/promptrails/parserails/actions/workflows/ci.yml)
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
actually need. On top of that it reconstructs the structure those pipelines
read: headings, tables and lists as [Markdown](./docs/markdown.md).

It also handles what arrives around the document. Real input is a scan, or a
`.docx` with a spreadsheet pasted into it, or an invoice attached to an e-mail
inside a ZIP — so ParseRails [detects formats from their
bytes](./docs/formats.md), [walks files inside
files](./docs/containers.md), and [says which pages need OCR](./docs/complexity.md)
before you pay for it.

## Features

| Capability | Engine | Status |
|------------|--------|--------|
| PDF text + per-word bounding boxes | PDFium (WASM) | ✅ |
| Word / line granularity, opt-in font size | PDFium (WASM) | ✅ |
| Page screenshot rendering (`RenderPage`) | PDFium (WASM) | ✅ |
| Pluggable OCR + automatic fallback | interface | ✅ |
| Figure OCR on text pages, merged with native words | interface | ✅ |
| Standalone images (PNG/JPEG/TIFF/BMP/WebP/GIF) | OCR backend | ✅ |
| Tesseract OCR adapter (cgo-free, subprocess) | `tesseract` CLI | ✅ |
| HTTP OCR adapter (LiteParse OCR API compatible) | `net/http` | ✅ |
| Office formats (DOCX/PPTX/XLSX/...) | LibreOffice headless → PDF | ✅ |
| Native DOCX/XLSX/PPTX reading (no LibreOffice) | `archive/zip` + `encoding/xml` | ✅ |
| Magic-byte format detection (`Sniff`/`Detect`) | — | ✅ |
| Encrypted PDFs, page ranges (`ReadOptions`) | PDFium | ✅ |
| Complexity routing (`Inspect`, `is-complex`) | PDFium | ✅ |
| Recursive containers (ZIP/EML/MSG/OLE/attachments) | — | ✅ |
| Concurrent batch parsing | — | ✅ |
| Per-document timeouts (`WithTimeout`) | — | ✅ |
| CLI: parse/batch/extract/render/is-complex, stdin | — | ✅ |
| `ExtractText` whole-page text fast path | PDFium | ✅ |
| Markdown output (headings, tables, lists, figures) | — | ✅ |
| Layout blocks as data (`Blocks`) | — | ✅ |
| Native cgo backend (`-tags parserails_cgo`) | libpdfium | ✅ |

## Choose the right entry point

| Task | Go | CLI |
|---|---|---|
| PDF/Office/image text with coordinates | `ParseData`, `ParseFile` | `parse --format json` |
| PDF text layer only, without OCR | `ExtractText`, `ExtractTextData` | — |
| Headings, tables and lists | `Document.Markdown`, `Document.Blocks` | `parse --format markdown` |
| DOCX/XLSX/PPTX structure without LibreOffice | `ReadOfficeDocument` | `parse --native-office --format markdown` |
| Archives, emails and embedded attachments | `Extract`, `ExtractFile` | `extract` |
| Independent files in a batch | `ParseFiles` | `batch` |
| OCR candidates and page screenshots | `Inspect`, `RenderPage` | `is-complex`, `render` |

For a complete walkthrough with runnable Go code, start with the
[Usage Guide](./docs/usage-guide.md). The [CLI reference](./docs/cli.md) lists
flags and exit codes.

## Install

Requires **Go 1.27 or later**.

As a library:

```bash
go get github.com/promptrails/parserails
```

As a command:

```bash
go install github.com/promptrails/parserails/cmd/parserails@latest

parserails parse      invoice.pdf               # reconstructed text
parserails parse      --format markdown doc.pdf  # headings, tables, lists
parserails parse      --json report.docx         # JSON with boxes
parserails batch      ./corpus ./out             # a directory, concurrently
parserails extract    bundle.zip                 # everything inside a file
parserails is-complex scan.pdf                   # which pages need OCR?
parserails render     --dpi 150 doc.pdf          # page 0 → doc-p0.png

curl -sL https://example.com/report.pdf | parserails parse -
```

No system dependencies for PDF or native DOCX/XLSX/PPTX text reading. Office
**page layout and word boxes** need `libreoffice`/`soffice` on PATH. OCR needs
the `tesseract` binary or a configured HTTP OCR server.

`@latest` installs a published version. To test the code in a local checkout:

```bash
go build -o ./bin/parserails ./cmd/parserails
./bin/parserails parse --native-office --format markdown report.docx
```

Place CLI flags before positional arguments. `--pages` selections are 1-based;
`render --page` and result page indexes are 0-based.

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

	pdf, err := os.ReadFile("invoice.pdf")
	if err != nil {
		panic(err)
	}

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
	Confidence     float64 // OCR score, 0-1; 0 for natively extracted text
}
```

### Granularity

Word-level boxes are precise but extract every character across the WASM
boundary. For per-line boxes at ~6× the speed:

```go
p, _ := parserails.New(parserails.WithGranularity(parserails.GranularityLine))
```

## Markdown & blocks

Structure, reconstructed from the geometry — headings, tables, lists, figures,
in reading order:

```go
md := doc.Markdown()

for _, b := range doc.Blocks() {
	fmt.Printf("%-10s p%d %q\n", b.Kind, b.Page, b.Text)
}
```

For PDF-derived blocks, Go coordinate fields map a heading or table cell back
to the page. Use `WithFontInfo()` and `WithImages()` to supply heading metrics
and figure regions; the CLI enables these for Markdown automatically. Figure
links are placeholders, not image files written to disk. See
[docs/markdown.md](./docs/markdown.md).

## Does it need OCR?

```go
c, _ := p.Inspect(ctx, data, parserails.ReadOptions{})
if c.NeedsOCR() {
	fmt.Println("pages needing OCR:", c.OCRPages()) // reasons: scanned, garbled, ...
}
```

A text-layer pass — nothing is rendered, no OCR runs — so a pipeline can route
or price a batch for a fraction of a parse. See
[docs/complexity.md](./docs/complexity.md).

## Files inside files

```go
node, err := p.ExtractFile(ctx, "bundle.zip", parserails.ExtractOptions{
	MaxFiles: 100,
	MaxBytes: 64 << 20,
})
if err != nil {
	panic(err)
}
node.Walk(func(n *parserails.Node) {
	if n.Err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", n.Name, n.Err)
	}
})
fmt.Println(node.AllText())
```

PDF attachments, ZIP entries, e-mail and Outlook attachments, OLE objects
embedded in office documents — recursively, bounded by depth, file count and
unpacked bytes. One unreadable file is recorded on its own node instead of
failing the walk. Check every `Node.Err` before treating the result as complete.
`Extract` returns an inventory/text tree; it does not save the original
attachment files. See [docs/containers.md](./docs/containers.md).

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
no `libtesseract`), and `ocr/httpocr` talks to a remote server over the
**LiteParse OCR API**, so the OCR server images published for LiteParse
(EasyOCR, PaddleOCR, RapidOCR, Surya) work unchanged. Implement the one-method
`OCR` interface for anything else.

`WithImageOCR` goes further than the fallback: it also reads the figures on
pages that *do* have text — charts, pasted screenshots — and merges the result
with the native words.

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

The PDF fast path never runs OCR, even with `WithOCR` configured. Use
`Parse`/`ParseData` for scanned PDFs and call `doc.Text()` on the result.

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

`ParseFile` converts Office documents to PDF via LibreOffice, then parses.
`ParseFiles` captures errors in each `FileResult` and preserves input order;
pass a positive concurrency value. `WithNativeOffice` applies to text
extraction and the container walk, not these spatial APIs.

For CLI input discovery, output collisions, retries and examples, see
[Batch Processing](./docs/batch.md).

## Limits and partial results

Extraction defaults to depth 8, 512 file attempts and 256 MiB of content,
including the root. `MaxBytes` is not a process memory cap: PDFium materializes
PDF attachments before their sizes can be checked. `WithTimeout` stops the
caller waiting for an operation but cannot kill an in-progress PDFium call.
Use a context deadline for a whole request spanning multiple operations.

CLI `extract` can exit successfully with errors on individual nodes. Use tree
or JSON output when completeness matters; text output alone omits those errors.
See [Limits & Timeouts](./docs/limits.md) for accounting, cancellation and native
Office timeout scope.

## Documentation

Full documentation lives in [`docs/`](./docs):

- [Getting Started](./docs/getting-started.md) and [Usage Guide](./docs/usage-guide.md) — installation and complete workflows.
- [CLI](./docs/cli.md) and [API Reference](./docs/api.md) — commands, flags, options and result types.
- [OCR](./docs/ocr.md), [Native Office](./docs/office.md), [Containers](./docs/containers.md) and [Batch Processing](./docs/batch.md) — format-specific usage.
- [Limits & Timeouts](./docs/limits.md) and [Troubleshooting](./docs/troubleshooting.md) — quotas, partial results and operational behavior.

The documentation site deploys from `main`. Feature-branch pages can be read
in the checkout or previewed locally as described in
[Troubleshooting](./docs/troubleshooting.md).

## Agent skill

[`SKILL.md`](./SKILL.md) is a ready-made skill file for coding agents: which
command fits which task, how to route scanned documents, and how to read the
output. Copy it into your agent's skills directory.

## Examples

Runnable, self-contained examples (each its own module + Dockerfile) live in
[`examples/`](./examples):

- [`extract-text`](./examples/extract-text) — the basics, fully static cgo-free binary
- [`parse-server`](./examples/parse-server) — HTTP service with OCR + office support
- [`native-cgo`](./examples/native-cgo) — native PDFium backend for max throughput

See [docs/examples.md](./docs/examples.md).

## Status

The PDF core is complete — spatial text, rendering, OCR, office formats, CLI,
batch — with both a cgo-free WASM backend and a native `parserails_cgo`
backend. Around it sit the layers a real pipeline needs: format detection,
[Markdown and block reconstruction](./docs/markdown.md), [OCR
routing](./docs/complexity.md), [recursive container
extraction](./docs/containers.md) and [native office
reading](./docs/office.md).

See [`docs/roadmap.md`](./docs/roadmap.md) for the detailed status and what is
still being considered (ruled-table detection from vector graphics, layout
complexity signals, published quality benchmark numbers).

## License

[MIT](./LICENSE) © PromptRails
