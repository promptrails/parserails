# Usage Guide

This guide follows a document from input to useful output: choose an API,
extract text or structure, enable OCR when needed, and handle attachments and
partial failures. For individual flags and signatures, see the [CLI](cli.md)
and [API reference](api.md).

## 1. Install and choose the input path

Use Go **1.27 or later**. The default PDF backend runs PDFium as WebAssembly;
you do not need a C toolchain or a system PDFium library.

```bash
# In your Go application's module:
go get github.com/promptrails/parserails

# Install the released command:
go install github.com/promptrails/parserails/cmd/parserails@latest

# To try the code in a local checkout, including unreleased changes:
go build -o ./bin/parserails ./cmd/parserails
./bin/parserails version
```

`@latest` installs the published version, not a feature branch. The remainder
of this guide uses `parserails` on PATH; substitute `./bin/parserails` when
testing a checkout.

| Input and desired result | CLI | Go API | Additional dependency |
|---|---|---|---|
| PDF words, boxes, or reconstructed text | `parse` | `ParseData` / `ParseFile` | none |
| PDF text layer only, without layout or OCR | — | `ExtractText` / `ExtractTextData` | none |
| PDF headings, tables, lists | `parse --format markdown` | `Document.Markdown`, `Document.Blocks` | none |
| Scanned PDF or standalone image | `parse --ocr tesseract` | `WithOCR`, then `ParseData` | Tesseract, or an HTTP OCR server |
| Office document with page coordinates | `parse --format json` | `ParseData` / `ParseFile` | LibreOffice |
| DOCX/XLSX/PPTX text or Markdown | `parse --native-office` | `ReadOfficeDocument`, or `WithNativeOffice` for text APIs | none |
| ZIP, email, or document attachments | `extract` | `Extract` / `ExtractFile` | depends on the embedded formats |
| A directory of PDFs, Office files, images | `batch` | `ParseFiles` | depends on the input formats |

Detection and parsing are different: recognizing a ZIP does not make it a
spatial document. Use `Extract` for ZIP/EML/MSG/plain text; `ParseData` accepts
PDF, Office and image inputs. See [Format Detection](formats.md).

## 2. Parse a document and retain its source coordinates

Save this complete example as `main.go` in your Go module:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/promptrails/parserails"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run . <document.pdf>")
	}
	p, err := parserails.New(
		parserails.WithFontInfo(),
		parserails.WithImages(),
		parserails.WithTimeout(30*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	doc, err := p.ParseFile(ctx, os.Args[1])
	if err != nil {
		log.Print(err)
		return
	}
	fmt.Print(doc.Markdown())
	for _, word := range doc.Words() {
		fmt.Fprintf(os.Stderr, "page=%d text=%q box=[%.1f %.1f %.1f %.1f]\n",
			word.Page, word.Text, word.X0, word.Y0, word.X1, word.Y1)
	}
}
```

```bash
go run . report.pdf > report.md
parserails parse --format markdown report.pdf > report.md
parserails parse --format json report.pdf > report.json
```

Create one `Parser` per process and reuse it across requests. Close it after
the callers using it have finished. The library supports concurrent calls.

For PDFs and Office documents converted to PDF, coordinates are **points**
with a **bottom-left** origin. Standalone images retain **pixels**, with the
same bottom-left origin. Page indexes in results start at zero.

## 3. Select pages and open encrypted PDFs

The following snippets assume a configured `p`, a `ctx`, and input `data`.
Use the methods ending in `Data` when you need per-read options:

```go
doc, err := p.ParseData(ctx, data, parserails.ReadOptions{
	Name:     "statement.pdf",
	Password: "document-password",
	Pages:    "2-4,8",
	MaxPages: 3,
})
if err != nil {
	return err
}
fmt.Println(doc.Text())
```

`Pages` uses **1-based** page numbers and inclusive ranges. Order is preserved,
duplicates are removed, and pages beyond the document are skipped. `MaxPages`
is applied after selection. In this example only pages 2, 3 and 4 are returned;
their `Page.Index` values remain 1, 2 and 3.

```bash
parserails parse --pages '2-4,8' --max-pages 3 statement.pdf
parserails parse --password 'document-password' encrypted.pdf
parserails render --page 1 --dpi 150 -o page-two.png statement.pdf
```

`render --page` uses a **0-based** index. Put CLI flags **before** positional
arguments. Stdin is supported by commands taking one file:

```bash
cat statement.pdf | parserails parse --pages 1-2 -
cat statement.pdf | parserails render --page 0 -o cover.png -
```

## 4. Choose text, Markdown, or blocks

| Output | Preserves | Important behavior |
|---|---|---|
| `doc.Text()` | reconstructed lines and pages | newline between lines, form feed (`\f`) between pages |
| `p.ExtractText(ctx, pdf)` | PDFium's text order | no boxes, layout reconstruction, or PDF OCR |
| `doc.Markdown()` | inferred headings, lists, tables, figures | layout heuristics; repeated headers/footers are removed |
| `doc.Blocks()` | structured regions and Go coordinate fields | useful for citations and downstream chunking |
| CLI `--format json` | pages, words, image regions | serializes `Document`, not `Blocks()` |

For Markdown from the Go API, configure `WithFontInfo()` for heading metrics
and `WithImages()` for figure regions. The CLI enables these automatically for
Markdown. Keep repeating page furniture when it is part of your content:

```go
markdown := doc.MarkdownWith(parserails.BlockOptions{
	KeepHeadersFooters: true,
})
fmt.Print(markdown)

for _, block := range doc.Blocks() {
	if block.Kind == parserails.BlockTable {
		fmt.Printf("page=%d header=%v rows=%v\n", block.Page, block.Header, block.Rows)
	}
}
```

Figure references such as `![](img_p1_2.png)` are **placeholders**. Markdown
output does not save image files. Render/crop regions yourself if you need
working image assets. Block and cell coordinate fields are available in Go
but omitted by their default JSON encoding; use your own response type to
export them. See [Blocks & Markdown](markdown.md).

## 5. Read scans and figures with OCR

For local OCR, install the `tesseract` executable and the language data you
intend to use. For a remote service, configure its OCR endpoint:

```bash
parserails parse --ocr tesseract --lang eng scan.pdf
parserails parse --ocr tesseract --lang tur receipt.jpg
parserails parse --ocr http --ocr-url http://localhost:8080/ocr --lang en scan.pdf
parserails parse --ocr tesseract --image-ocr --format markdown report.pdf
```

The same configuration in Go, importing `ocr/tesseract` from this module:

```go
p, err := parserails.New(
	parserails.WithOCR(tesseract.New(tesseract.Config{Lang: "eng"})),
	parserails.WithImageOCR(),
	parserails.WithTimeout(30*time.Second),
)
```

`WithOCR` performs whole-page fallback only when a PDF page has **zero words**.
`WithImageOCR` additionally reads substantial raster figures on text pages.
It needs an OCR backend; it does not install one. A standalone image requires
OCR, while a PDF without a backend can return an empty page.

To estimate which pages need attention before parsing:

```go
inspection, err := p.Inspect(ctx, data, parserails.ReadOptions{Name: "scan.pdf"})
if err != nil {
	return err
}
fmt.Println("zero-based OCR candidates:", inspection.OCRPages())
```

`Inspect` does not run OCR. Its verdict is a heuristic and does not automatically
force the OCR fallback on sparse or garbled text. `ExtractText` on a PDF never
runs OCR, even if the parser has an OCR backend. Use the parsing path when you
need recognized text. See [OCR](ocr.md) and [Complexity & Routing](complexity.md).

## 6. Read Office text without LibreOffice

```bash
parserails parse --native-office report.docx
parserails parse --native-office --format markdown workbook.xlsx
parserails parse --native-office --format markdown presentation.pptx
```

The direct reader does not require a `Parser` or a PDFium pool:

```go
office, err := parserails.ReadOfficeDocument(data, parserails.FormatDOCX)
if err != nil {
	return err
}
fmt.Print(office.Markdown())
```

It returns author-provided structure: DOCX paragraphs/tables, XLSX cell values,
and PPTX text in slide order. It does not lay out pages, calculate formulas,
or provide word boxes. `Pages`/`MaxPages` do not select native OOXML content.

For automatic text routing, create a parser with `WithNativeOffice()` and use
`ExtractTextData`, `ExtractFileText`, or `Extract`. `ParseData` and `ParseFile`
still need LibreOffice for Office inputs because they produce spatial output.
Native reading covers DOCX/XLSX/PPTX; legacy Office and OpenDocument conversion
still use LibreOffice. See [Office Formats](office.md).

## 7. Walk an archive or email and handle partial results

```bash
parserails extract --format tree bundle.zip
parserails extract --format json --list mail.eml > inventory.json
parserails extract --native-office --format text mail.msg > content.txt
```

`extract` returns an in-memory tree and its text; it does **not** write the
original attachment bytes to disk. Use JSON or the Go tree when you need to
retain provenance and failure information:

```go
node, err := p.Extract(ctx, data, parserails.ExtractOptions{
	ReadOptions: parserails.ReadOptions{Name: "bundle.zip"},
	MaxDepth:    4,
	MaxFiles:    100,
	MaxBytes:    64 << 20,
})
if err != nil {
	return err
}
node.Walk(func(n *parserails.Node) {
	if n.Err != nil {
		log.Printf("partial extraction: %s: %v", n.Name, n.Err)
	}
})
fmt.Println(node.AllText())
```

Check both the returned error and **every `Node.Err`**, including the root.
A successful call can contain rejected or unreadable children. The CLI also
may exit successfully with node errors; plain-text output loses those details.
Use `--format json` or `--format tree` when completeness matters.

`SkipParse`/`--list` still opens containers and unpacks their contents to find
nested files. It skips document text parsing, not decompression. Identical
content is visited only once across the tree. See [Containers](containers.md)
for MIME, Outlook and OLE behavior, and [Limits & Timeouts](limits.md) for
exact budget semantics and the PDF attachment memory limitation.

## 8. Process a directory

```bash
parserails batch --format markdown --concurrency 4 ./input ./output
parserails batch --native-office --ext .docx ./input ./output
```

The output tree mirrors the input tree. Existing output files may be replaced,
and individual failures are reported while the batch continues. The final exit
code is nonzero if any selected document failed. ZIP and email files are not
batch inputs; use recursive extraction for those.

See [Batch Processing](batch.md) for library examples, output name collisions,
input filtering, concurrency and exit-status handling.

## 9. Set a whole-request deadline

`WithTimeout` bounds individual internal operations. A conversion followed by
a PDF parse, or a tree with many children, can take longer than that value in
total. Pass a context deadline for your whole request:

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
defer cancel()
node, err := p.ExtractFile(ctx, "bundle.zip", parserails.ExtractOptions{
	MaxFiles: 100,
	MaxBytes: 64 << 20,
})
```

PDFium work already running cannot be killed by a Go deadline. The caller can
stop waiting while the worker remains occupied. Time limits, extraction byte
budgets and process memory limits serve different purposes; see
[Limits & Timeouts](limits.md). For unexpected output or dependency errors,
use [Troubleshooting](troubleshooting.md).
