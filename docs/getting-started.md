# Getting Started

## Installation

```bash
go get github.com/promptrails/parserails
```

Requires Go 1.27 or later. There are **no system dependencies** — PDFium ships as
a WebAssembly module that is loaded at runtime by [wazero](https://wazero.io), so
you do not need `CGO_ENABLED=1`, a C toolchain, or a `libpdfium.so` on the host.

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/promptrails/parserails"
)

func main() {
	// New() initializes a pooled PDFium WASM runtime. Create one per process.
	p, err := parserails.New()
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

## The result

`Parse` returns a `*Document`:

```go
type Document struct {
	Pages []Page
}

func (d *Document) Words() []Word // every page's words, flattened
func (d *Document) Text() string  // all words, space-joined
```

Each `Page` carries its dimensions (in points) and its words; each `Word` carries
its text, page index, bounding box, and font size. See
[Spatial Text Extraction](parsing.md) for the coordinate system and details.

## Tuning the pool

PDF parsing is CPU-bound. The parser borrows a worker from a pool per `Parse`
call; size it to your concurrency:

```go
p, _ := parserails.New(
	parserails.WithPoolSize(2, 4, 8), // minIdle, maxIdle, maxTotal
)
```

## Where to go next

The quick start above is the spatial core. The rest of the library is about
what arrives around a PDF:

| You want | Read |
|----------|------|
| text with its lines and pages intact | [Spatial Text Extraction](parsing.md) |
| headings, tables and lists for an LLM | [Blocks & Markdown](markdown.md) |
| to know whether a document needs OCR | [Complexity & Routing](complexity.md) |
| to read scans, or figures inside reports | [OCR](ocr.md), [Images as Input](images.md) |
| Word/Excel/PowerPoint, with or without LibreOffice | [Office Formats & Batches](office.md) |
| what is inside a ZIP, an e-mail, an attachment | [Containers](containers.md) |
| to identify what a byte slice even is | [Format Detection](formats.md) |
| the command line | [CLI](cli.md) |
| every exported name in one place | [API Reference](api.md) |
