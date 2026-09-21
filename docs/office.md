# Office Formats & Batches

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

> **Why LibreOffice instead of a native XLSX reader?** Converting to PDF yields
> real spatial coordinates uniformly across every format, and adds **zero Go
> dependencies** (it's a subprocess). A native cell-level XLSX reader is on the
> roadmap for callers who want spreadsheet structure instead of layout.

## Batch parsing

`ParseFiles` parses many files concurrently, bounded by a worker count, and
captures per-file errors instead of aborting the batch.

```go
results := p.ParseFiles(ctx, paths, 4) // up to 4 at once
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

Text extraction falls back to native reading **by itself** when LibreOffice is
missing and the input is an OOXML package, so a container walk through an
archive of `.docx` files still produces text on a machine with no LibreOffice
installed. A missing binary is reported as `ErrNoLibreOffice`, which is
distinguishable from a broken document:

```go
if errors.Is(err, parserails.ErrNoLibreOffice) { ... }
```
