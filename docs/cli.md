# CLI

ParseRails ships a command-line tool.

```bash
go install github.com/promptrails/parserails/cmd/parserails@latest
```

The PDF engine is cgo-free, so the binary has no native dependencies. Office
conversion needs `libreoffice`/`soffice` on PATH; OCR needs `tesseract` (only
when you enable it).

## parse

Extract spatial text from a PDF or office document.

```bash
parserails parse invoice.pdf                  # one line of text per word
parserails parse --json invoice.pdf           # full JSON document with boxes
parserails parse --granularity line notes.pdf # line-level boxes (faster)
parserails parse --font invoice.pdf           # include font sizes
parserails parse --ocr tesseract scan.pdf     # OCR scanned pages
parserails parse report.docx                  # office doc via LibreOffice
```

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | off | emit the JSON document (pages, words, boxes) |
| `--granularity` | `word` | `word` or `line` |
| `--font` | off | collect font sizes (word granularity) |
| `--ocr` | `none` | `none` or `tesseract` |
| `--lang` | `eng` | OCR language |

## render

Rasterize a PDF page to PNG.

```bash
parserails render document.pdf                # page 0 → document-p0.png
parserails render --page 2 --dpi 200 doc.pdf  # page 2 at 200 DPI
parserails render -o out.png doc.pdf          # explicit output path
```

| Flag | Default | Description |
|------|---------|-------------|
| `--page` | `0` | page index (0-based) |
| `--dpi` | `150` | render resolution |
| `-o` | `<file>-p<page>.png` | output path |

## version

```bash
parserails version
```

## `parserails parse --format`

| Value | Output |
|-------|--------|
| `text` (default) | reconstructed plain text, lines and page breaks intact |
| `json` | the full `Document`: pages, words, boxes |
| `markdown` | [headings, tables, lists and figures](markdown.md) |

`--format markdown` turns on font metrics and image collection by itself, since
heading ranking and figure placement need them. `--keep-headers-footers` keeps
running headers, footers and page numbers that are otherwise dropped.

## `parserails extract`

Walk a file and everything inside it — see [Containers](containers.md).

| Flag | Meaning |
|------|---------|
| `--format` | `tree` (default), `text`, `json` |
| `--max-depth` | how deep to descend (0 = default 8, -1 = no limit) |
| `--max-files` | cap how many files are opened (0 = default 512) |
| `--list` | inventory only; do not parse the documents |
| `--ocr`, `--lang` | OCR backend for scanned pages and image attachments |
| `--password` | password for encrypted documents |

## `parserails is-complex`

Report, page by page, whether a document needs OCR. Per-page JSON goes to
stdout and a one-line verdict to stderr.

```bash
parserails is-complex [flags] <file>   # or "-" to read stdin
```

| Flag | Meaning |
|------|---------|
| `--compact` | dense JSON instead of indented |
| `--pages` | 1-based selection, e.g. `"1-5,10"` |
| `--max-pages` | cap how many pages are inspected |
| `--password` | password for encrypted documents |
| `-q` | suppress the stderr verdict |

Exit codes: `0` simple, `2` at least one page needs OCR, `1` error. See
[Complexity & Routing](complexity.md).
