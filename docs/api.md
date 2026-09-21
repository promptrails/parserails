# API Reference

Everything ParseRails exports, grouped by what you are trying to do. Each entry
links to the page that explains it properly.

## Creating a parser

One `Parser` per process: it owns a pooled PDFium runtime and is safe for
concurrent use.

```go
p, err := parserails.New(opts...)
defer p.Close()
```

| Option | Effect |
|--------|--------|
| `WithGranularity(GranularityWord\|GranularityLine)` | box precision vs. speed — [parsing](parsing.md) |
| `WithFontInfo()` | collect per-word font sizes |
| `WithPassword(string)` | default password for encrypted documents |
| `WithMaxPages(int)` | cap pages per read |
| `WithOCR(OCR)` | OCR backend for scanned pages — [OCR](ocr.md) |
| `WithImageOCR()` | also read figures on pages that have text |
| `WithImages()` | record each page's raster figures (`Page.Images`) |
| `WithNativeOffice()` | read OOXML without LibreOffice — [office](office.md) |
| `WithLibreOffice(path)` | explicit `soffice` binary |
| `WithContainer(Format, Container)` | teach or replace a container — [containers](containers.md) |
| `WithTimeout(time.Duration)` | bound each document — [architecture](architecture.md) |
| `WithPoolSize(min, maxIdle, maxTotal)` | tune the PDFium pool |

`Backend` is a constant naming the active engine: `"wasm"` or `"cgo"`.

## Reading a document

| Call | Returns | Use for |
|------|---------|---------|
| `Parse(ctx, pdf)` | `*Document` | PDF bytes, parser defaults |
| `ParseData(ctx, data, ReadOptions)` | `*Document` | any format, detected from the bytes |
| `ParseFile(ctx, path)` | `*Document` | a path (office documents converted) |
| `ParseFiles(ctx, paths, concurrency)` | `[]FileResult` | a bounded concurrent batch |
| `ParseImage(ctx, data)` | `*Document` | a scan or photo, via OCR — [images](images.md) |
| `ExtractText(ctx, pdf)` | `string` | text only, several times cheaper |
| `ExtractTextData(ctx, data, ReadOptions)` | `string` | text only, any format |
| `ExtractFileText(ctx, path)` | `string` | text only, from a path |
| `RenderPage(ctx, pdf, RenderRequest)` | `image.Image` | a page raster — [screenshots](screenshots.md) |
| `Inspect(ctx, data, ReadOptions)` / `InspectFile` | `*Complexity` | does this need OCR? — [complexity](complexity.md) |
| `Extract(ctx, data, ExtractOptions)` / `ExtractFile` | `*Node` | files inside files — [containers](containers.md) |

`ReadOptions{Name, Password, Pages, MaxPages}` tunes one read; `ExtractOptions`
embeds it and adds `MaxDepth`, `MaxFiles`, `MaxBytes`, `SkipParse`.
`FileResult{Path, Document, Err}` keeps a batch's per-file errors.

## What comes back

| Type | Is |
|------|----|
| `Document` | `Pages`; `Words()`, `Lines()`, `Text()`, `Blocks()`, `Markdown()` |
| `Page` | `Index`, `Width`, `Height`, `Words`, `Images`; `Lines()`, `Text()` |
| `Word` | `Text`, `Page`, `X0,Y0,X1,Y1`, `FontSize`, `Confidence`; `IsOCR()` |
| `Line` | a reconstructed line: box, `Words`; `Text()`, `Height()`, `FontSize()` |
| `Block` | a classified region — [blocks & Markdown](markdown.md) |
| `BlockKind` | `BlockHeading`, `BlockParagraph`, `BlockListItem`, `BlockTable`, `BlockFigure` |
| `Cell` | a table cell: `Text` plus its own box |
| `ImageRegion` | a raster figure on a page: `Index`, box, `Area()` |
| `Complexity` / `PageComplexity` | the OCR verdict, with `Reasons` |
| `Node` | one file in an extraction tree: `Document`, `Text`, `Children`, `Err` |
| `OfficeDocument` | an OOXML package read natively: `Blocks`; `Text()`, `Markdown()` |

Coordinates are PDF points with a bottom-left origin, everywhere.

## Formats

```go
parserails.Sniff(data)              // content only
parserails.Detect(name, data)       // content, name as tiebreaker
parserails.FormatByName("a.docx")   // name only
parserails.IsOfficeFormat(path)     // does this need LibreOffice?
```

`Format` has `IsImage()`, `IsOffice()`, `IsOOXML()`, `Ext()`, `String()`, and
constants from `FormatPDF` to `FormatUnknown` — [format detection](formats.md).

## Extension points

```go
// An OCR engine: pixel-space boxes in, ParseRails maps them back to points.
type OCR interface {
	Recognize(ctx context.Context, img image.Image) ([]Word, error)
}

// A container: one level of embedded files, within the walk's budget.
type Container interface {
	Children(ctx context.Context, data []byte, req ChildRequest) ([]Child, error)
}
```

Bundled OCR backends live in [`ocr/tesseract`](ocr.md) and
[`ocr/httpocr`](ocr.md). `Child{Name, Data, Err}` and
`ChildRequest{Name, Password, MaxFiles, MaxBytes}` carry a container's input
and output; `ReadOfficeDocument(data, Format)` is the native OOXML reader.

## Errors worth matching

```go
errors.Is(err, parserails.ErrNoLibreOffice) // the binary is missing, the document is fine
```

Encrypted documents fail with `document is encrypted` or `wrong password`; a
document over `WithTimeout` fails with `document timed out after …`.
