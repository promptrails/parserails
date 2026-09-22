# Format Detection

ParseRails identifies a document from its **bytes**, not its name. Extensions
lie: a DOCX saved as `.pdf`, an attachment called `invoice.dat`, or a `[]byte`
pulled out of a ZIP with no name at all are all routine inputs.

```go
format := parserails.Sniff(data)              // content only
format = parserails.Detect("invoice.pdf", data) // content, name as tiebreaker
format = parserails.FormatByName("invoice.pdf") // name only
```

## What is recognized

| Group | Formats |
|-------|---------|
| PDF | `pdf` |
| Office Open XML | `docx`, `xlsx`, `pptx` |
| OpenDocument | `odt`, `ods`, `odp` |
| Legacy OLE (Compound File Binary) | `doc`, `xls`, `ppt`, `msg` (as `ole` when unnamed) |
| Images | `png`, `jpeg`, `gif`, `tiff`, `bmp`, `webp` |
| Containers & text | `zip`, `eml`, `rtf`, `txt` |

Predicates keep routing readable:

```go
format.IsImage()  // raster image — read through OCR
format.IsOffice() // converted to PDF before parsing
format.IsOOXML()  // ZIP + XML package
format.Ext()      // ".docx"
```

## Content wins, with one exception

`Detect` prefers what the bytes say. The exception is the legacy **Compound File
Binary** container: `.doc`, `.xls`, `.ppt` and `.msg` share one signature, so
only the file name tells them apart. Without a name they stay `ole`.

When the bytes are inconclusive — plain text, or a format ParseRails has no
magic number for — the extension is used as a fallback.

## Parsing detected input

`ParseData` runs detection and routes accordingly; `ParseFile` does the same for
a path, converting office documents straight from disk.

```go
doc, err := p.ParseData(ctx, data, parserails.ReadOptions{Name: "attachment.docx"})
doc, err = p.ParseFile(ctx, "/tmp/report.dat") // detected as PDF if that's what it is
```

`Parse` stays a strict PDF entry point: it rejects data that sniffs as another
format with an error naming what it found, instead of handing PDFium something
it cannot open.
