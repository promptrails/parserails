# ParseRails

> Document parsing, structured output and recursive extraction for Go.

[![CI](https://github.com/promptrails/parserails/actions/workflows/ci.yml/badge.svg)](https://github.com/promptrails/parserails/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/promptrails/parserails.svg)](https://pkg.go.dev/github.com/promptrails/parserails)

ParseRails reads PDF text with bounding boxes, reconstructs Markdown and
layout blocks, routes scanned pages to OCR, reads native Office structure,
and walks files inside archives, emails and document attachments. Its default
PDFium backend runs as WebAssembly under Go, without cgo or a system PDFium
installation. OCR and Office conversion are optional integrations.

## Start here

- **[Getting Started](getting-started.md)** — install the library or CLI and parse your first PDF.
- **[Usage Guide](usage-guide.md)** — a complete walkthrough with Go and CLI examples, output choices, OCR, Office and attachments.
- **[CLI Reference](cli.md)** — commands, flags, page numbering and exit codes.
- **[API Reference](api.md)** — parser options, entry points and result types.

## Choose a workflow

| You need | Read |
|---|---|
| Words and source coordinates | [Spatial Text Extraction](parsing.md) |
| Headings, tables, lists and figure placeholders | [Blocks & Markdown](markdown.md) |
| Detect the format of an uploaded file | [Format Detection](formats.md) |
| Read scanned pages or figures | [OCR](ocr.md), [Images as Input](images.md) |
| Inspect pages before choosing an OCR path | [Complexity & Routing](complexity.md) |
| DOCX/XLSX/PPTX text without LibreOffice, or Office page layout with it | [Office Formats](office.md) |
| Walk ZIP, EML, MSG, PDF attachments or OLE objects | [Containers](containers.md) |
| Process a directory and handle individual failures | [Batch Processing](batch.md) |
| Render a PDF page to PNG | [Page Screenshots](screenshots.md) |
| Configure quotas, deadlines and partial-result handling | [Limits & Timeouts](limits.md) |
| Diagnose missing dependencies or unexpected output | [Troubleshooting](troubleshooting.md) |

## Install

Requires **Go 1.27 or later**:

```bash
go get github.com/promptrails/parserails
go install github.com/promptrails/parserails/cmd/parserails@latest

parserails parse --format markdown report.pdf
parserails parse --native-office workbook.xlsx
parserails extract --format json bundle.zip
```

PDF and native DOCX/XLSX/PPTX text reading require no external binaries.
Office page coordinates require LibreOffice. OCR requires Tesseract or a
configured server. See the [dependency matrix](usage-guide.md).

## Integration and operations

Reuse one `Parser` per process and close it after in-flight calls finish.
Inspect each extraction node's error; a successful `Extract` call can contain
partial results. The PDF text fast path does not run OCR. Timeouts stop the
caller waiting but cannot kill an in-progress PDFium call. These distinctions
are explained with examples in the [Usage Guide](usage-guide.md) and
[Limits & Timeouts](limits.md).

Runnable applications and Dockerfiles are in [Examples](examples.md).
For engine/pool design, performance measurements and remaining work, see
[Architecture](architecture.md), [Benchmarks](benchmarks.md) and [Roadmap](roadmap.md).

## Which documentation am I reading?

These Markdown files describe the source checkout they belong to. The
published Docsify site is updated from `main`; feature-branch changes appear
there after merge and a successful docs deployment. An installed `@latest`
CLI may be an older published version. To preview branch docs or build that
branch's CLI, follow [Troubleshooting](troubleshooting.md).
