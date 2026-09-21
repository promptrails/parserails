# CLI

ParseRails ships a command-line tool.

```bash
go install github.com/promptrails/parserails/cmd/parserails@latest
```

The PDF engine is cgo-free, so the binary has no native dependencies.
LibreOffice is only needed for office formats — and only when you do not use
`--native-office`; OCR needs `tesseract`, or a server with `--ocr http`.

```
parserails parse      [flags] <file>        text, JSON or Markdown
parserails batch      [flags] <in> <out>    parse a directory, concurrently
parserails extract    [flags] <file>        walk a file and everything inside it
parserails render     [flags] <file>        render a page to a PNG image
parserails is-complex [flags] <file>        report which pages need OCR
parserails version
```

Every command that takes an input file accepts `-` for standard input:

```bash
curl -sL https://example.com/report.pdf | parserails parse -
```

## Shared flags

`parse`, `batch` and `extract` share the flags that open a document:

| Flag | Default | Description |
|------|---------|-------------|
| `--ocr` | `none` | `none`, `tesseract`, or `http` |
| `--ocr-url` | — | OCR server URL, with `--ocr http` |
| `--lang` | `eng` | OCR language |
| `--image-ocr` | off | also [read figures](ocr.md) on pages that have text |
| `--native-office` | off | read [DOCX/XLSX/PPTX natively](office.md), without LibreOffice (text and Markdown output only) |
| `--password` | — | password for encrypted documents |
| `--timeout` | `0` | give up on a document after this long |

## parse

```bash
parserails parse invoice.pdf                      # reconstructed plain text
parserails parse --format json invoice.pdf        # pages, words, boxes
parserails parse --format markdown report.pdf     # headings, tables, lists
parserails parse --pages 1-5,10 long.pdf          # only these pages
parserails parse --granularity line notes.pdf     # line-level boxes (faster)
parserails parse --ocr tesseract scan.pdf         # OCR scanned pages
parserails parse report.docx                      # office doc via LibreOffice
```

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | `text`, `json` or `markdown` |
| `--json` | off | shorthand for `--format json` |
| `--pages` | all | 1-based selection, e.g. `"1-5,10"` |
| `--max-pages` | — | cap how many pages are parsed |
| `--granularity` | `word` | `word` or `line` |
| `--font` | off | collect font sizes (word granularity) |
| `--keep-headers-footers` | off | keep running headers and footers in Markdown |

`--format markdown` turns on font metrics and image collection by itself,
since heading ranking and figure placement need them. See
[Blocks & Markdown](markdown.md).

`--native-office` applies to `--format text` and `--format markdown`, where no
page layout is needed. `--format json` reports word boxes, which only exist
once a page has been laid out, so it still needs LibreOffice and says so.

## batch

Parse a directory of documents concurrently, mirroring the input tree into the
output directory.

```bash
parserails batch ./invoices ./text
parserails batch --format markdown --concurrency 8 ./corpus ./md
parserails batch --ext .pdf --recursive=false ./inbox ./out
```

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | `text` (`.txt`), `json` (`.json`) or `markdown` (`.md`) |
| `--ext` | all | only process this extension |
| `--recursive` | `true` | descend into subdirectories |
| `--concurrency` | CPU count | documents parsed at once |
| `-q` | off | only report failures |

A document that fails is reported on stderr and the batch continues; the exit
status is non-zero if any failed.

Output names mirror the input tree with the extension replaced. Two inputs
that would collide — `report.pdf` and `report.docx` in one directory both want
`report.txt` — keep their extension instead (`report.pdf.txt`,
`report.docx.txt`), so neither silently overwrites the other.

## extract

Walk a file and everything inside it — see [Containers](containers.md).

```bash
parserails extract bundle.zip                  # a tree view
parserails extract --format text mail.eml      # all the text, depth first
parserails extract --format json --list a.zip  # inventory, nothing parsed
```

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `tree` | `tree`, `text` or `json` |
| `--max-depth` | `8` | how deep to descend (`-1` = no limit) |
| `--max-files` | `512` | cap how many files are opened |
| `--list` | off | inventory only; do not parse the documents |

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
| `--password` | — | password for encrypted documents |
| `-o` | `<file>-p<page>.png` | output path (required when reading stdin) |

## is-complex

Report, page by page, whether a document needs OCR. Per-page JSON goes to
stdout and a one-line verdict to stderr.

```bash
parserails is-complex report.pdf
parserails is-complex --compact report.pdf | jq '[.[] | select(.needs_ocr) | .page]'
parserails is-complex -q report.pdf && parserails parse report.pdf
```

| Flag | Default | Description |
|------|---------|-------------|
| `--compact` | off | dense JSON instead of indented |
| `--pages` | all | 1-based selection |
| `--max-pages` | — | cap how many pages are inspected |
| `--password` | — | password for encrypted documents |
| `-q` | off | suppress the stderr verdict |

Exit codes: `0` simple, `2` at least one page needs OCR, `1` error. See
[Complexity & Routing](complexity.md).

## version

```bash
parserails version
```
