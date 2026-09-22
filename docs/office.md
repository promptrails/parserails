# Office Formats

## Office documents

ParseRails parses Word, PowerPoint, Excel, and OpenDocument files by converting
them to PDF with **headless LibreOffice**, then running the normal PDF pipeline.
The result is the same spatial `Document` you get from a PDF — positioned words
with bounding boxes — so office and PDF inputs are interchangeable downstream.

```go
doc, err := p.ParseFile(ctx, "quarterly.docx")
```

Recognized extensions: `.docx .doc .pptx .ppt .xlsx .xls .odt .odp .ods .rtf`.
Use `parserails.IsOfficeFormat(path)` to test a path.

### Requirements

`libreoffice` (or `soffice`) must be on PATH. Override the binary with:

```go
p, _ := parserails.New(parserails.WithLibreOffice("/opt/libreoffice/program/soffice"))
```

…or set the `PARSERAILS_SOFFICE` environment variable. Each conversion runs with
a throwaway LibreOffice user profile, so concurrent conversions don't collide.

Converting to PDF provides page coordinates uniformly across these formats.
Native DOCX/XLSX/PPTX reading is also available now: choose it when you need
document structure and text without page layout, as described below.

## Batch parsing

`ParseFiles` parses many files concurrently, bounded by a worker count, and
captures per-file errors instead of aborting the batch.

```go
results := p.ParseFiles(ctx, paths, 4) // up to 4 at once, []FileResult
for _, r := range results {
	if r.Err != nil {
		log.Printf("%s: %v", r.Path, r.Err)
		continue
	}
	fmt.Printf("%s: %d pages\n", r.Path, len(r.Document.Pages))
}
```

Results are returned in the same order as the input paths. A single `Parser` is
safe to share across the batch — it draws workers from its PDFium pool, which you
can size with `WithPoolSize`.

`ParseFiles` calls the spatial parsing path, so `WithNativeOffice` does not
remove its LibreOffice requirement. For directory discovery, CLI output
names, native-text batches and error handling, see [Batch Processing](batch.md).

## Without LibreOffice: reading OOXML natively

LibreOffice is a heavy dependency: hundreds of megabytes in an image, a
subprocess per document, a profile per conversion. When you want the **text**
rather than the page geometry, ParseRails can read an Office Open XML package
directly instead.

```go
office, err := parserails.ReadOfficeDocument(data, parserails.FormatDOCX)
fmt.Println(office.Text())
fmt.Println(office.Markdown())
```

```go
type OfficeDocument struct {
	Format Format
	Blocks []Block // the same blocks as Markdown output, without boxes
}
```

Or let the parser prefer it wherever text is what is being asked for —
`ExtractTextData`, `ExtractFileText`, and the [`Extract`](containers.md) walk:

```go
p, _ := parserails.New(parserails.WithNativeOffice())
```

| | LibreOffice conversion | Native package reading |
|---|---|---|
| Word boxes, page layout | ✅ | ❌ — nothing has been laid out |
| Headings | inferred from font size | **declared** by the document's styles |
| Tables | inferred from alignment | **real cells**, as authored |
| Formats | DOCX, XLSX, PPTX, DOC, XLS, PPT, ODT, ODS, ODP, RTF | DOCX, XLSX, PPTX |
| Dependency | `soffice` on PATH | none |

The trade is structure for geometry: a DOCX says which paragraph is a heading
and where a table's cells are, so none of it has to be inferred from
positions — but it has no coordinates at all. Use `ParseFile` when you need
boxes, `ReadOfficeDocument` when you need text and structure.

### Native format behavior

| Format | Native output | Limits of that representation |
|---|---|---|
| DOCX | paragraphs, heading styles, list items, tables | no page layout; nested table text is flattened into its containing cell |
| XLSX | worksheet headings and cell rows; shared/inline strings and stored values | no formula recalculation or Excel number/date formatting; sparse grids may be compacted |
| PPTX | slide text in presentation order; first paragraph becomes a heading | no rendered slides, positioned shapes or visual reading-order inference |

```bash
parserails parse --native-office --format markdown report.docx
parserails parse --native-office workbook.xlsx
parserails batch --native-office --format markdown --ext .pptx ./decks ./text
```

The direct `ReadOfficeDocument` function requires the correct `Format`; use
`Detect(name, data)` when it is not already known. It takes no `ReadOptions`,
context, or timeout. Native package text has no page boundaries, so PDF page
selection and page-count limits do not apply. See [Limits & Timeouts](limits.md)
for the difference between the direct function and parser-managed text reads.

A worksheet is expanded into a grid only while that grid is reasonably full: a
sheet whose handful of values sit in the last column would otherwise become
16384 cells a row, so such a sheet is compacted onto the columns that carry
something (the blank columns are lost, the values are not). A sheet too sparse
even for that — one value per row, each in a different column — is left
ragged, every row holding only its own values in column order. Alignment is
what gets given up under pressure; the data is not.

Text extraction falls back to native reading **by itself** when LibreOffice is
missing and the input is an OOXML package, so a container walk through an
archive of `.docx` files still produces text on a machine with no LibreOffice
installed. A missing binary is reported as `ErrNoLibreOffice`, which is
distinguishable from a broken document:

```go
if errors.Is(err, parserails.ErrNoLibreOffice) { ... }
```
