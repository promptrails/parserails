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

Flags must precede positional arguments: `parserails parse --format json file.pdf`.
`--help` displays the flags for one command. `@latest` is a published version;
use `go build -o ./bin/parserails ./cmd/parserails` to test a local checkout.
The [Usage Guide](usage-guide.md) combines commands into complete workflows.

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
| `--timeout` | `0` | per-operation timeout, e.g. `30s`; zero adds no limit — [scope and native-path exception](limits.md) |

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
page layout is needed. Combining `parse --native-office --format json` for
OOXML is an error: word boxes require LibreOffice, so drop `--native-office`
for JSON. Native output does not apply `--pages` or `--max-pages`.

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

Input discovery accepts PDF, Office and image extensions; it skips ZIP, email
and plain text. There are no batch `--pages` or `--max-pages` flags. With
`--native-office --format json`, batch uses spatial parsing and still requires
LibreOffice.

A document that fails is reported on stderr and the batch continues; the exit
status is non-zero if any failed.

Output names mirror the input tree with the extension replaced. Two inputs
that would collide — `report.pdf` and `report.docx` in one directory both want
`report.txt` — keep their extension instead (`report.pdf.txt`,
`report.docx.txt`), so neither silently overwrites the other within that run.
Further collisions receive numeric suffixes. Existing destination files from
previous runs may be overwritten. See [Batch Processing](batch.md).

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

`extract` emits a tree or recovered text; it does not save attachment files.
`--list` still unpacks containers, but skips parsing the documents they hold.
There is no CLI `--max-bytes` flag; the walk uses the library's 256 MiB default.
Set `ExtractOptions.MaxBytes` in Go when you need a different byte budget.

Node errors are included in tree/JSON output, and do not necessarily make the
command exit nonzero. Inspect them when completeness matters. Plain-text output
contains recovered content without an error report. See [Limits & Timeouts](limits.md).

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
