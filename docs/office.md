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
