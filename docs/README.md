# ParseRails

> Fast, light, cgo-free document parsing for Go.

ParseRails extracts text **with spatial bounding boxes** from PDFs, renders page
screenshots, and supports pluggable OCR — without cloud services, without an LLM,
and without `CGO_ENABLED=1`.

```go
p, _ := parserails.New()
defer p.Close()

doc, _ := p.Parse(ctx, pdfBytes)
for _, w := range doc.Words() {
    fmt.Printf("p%d %q [%.1f %.1f %.1f %.1f]\n", w.Page, w.Text, w.X0, w.Y0, w.X1, w.Y1)
}
```

## Why ParseRails

| | |
|---|---|
| **Spatial** | Every word carries its bounding box, page, and font size — not a flat string |
| **cgo-free** | PDFium runs as WebAssembly via [wazero](https://wazero.io); no native libs to install |
| **Same engine as the big tools** | Google's PDFium, the engine behind Chrome's PDF viewer |
| **Pluggable OCR** | Fall back to Tesseract or a remote OCR server for scanned pages |
| **Pooled** | A reusable, concurrency-safe runtime pool — one `Parser` per process |

ParseRails is the Go counterpart to [run-llama/liteparse](https://github.com/run-llama/liteparse).

## Install

Library:

```bash
go get github.com/promptrails/parserails
```

Command:

```bash
go install github.com/promptrails/parserails/cmd/parserails@latest
```

No system dependencies for PDF (Go 1.26+). Office formats need LibreOffice on
PATH; OCR needs the `tesseract` binary when enabled.

## Quick Links

- [Getting Started](getting-started.md) · [CLI](cli.md)
- [Spatial Text Extraction](parsing.md) · [Page Screenshots](screenshots.md)
- [OCR](ocr.md) · [Office Formats & Batches](office.md)
- [Architecture](architecture.md) · [Benchmarks](benchmarks.md) · [Roadmap](roadmap.md)
- [GitHub Repository](https://github.com/promptrails/parserails)
- [Go Package Reference](https://pkg.go.dev/github.com/promptrails/parserails)
