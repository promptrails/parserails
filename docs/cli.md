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
