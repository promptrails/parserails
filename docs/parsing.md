# Spatial Text Extraction

ParseRails keeps the **layout** of a document. Where most Go PDF readers hand you
a flat string, ParseRails reports every word with its bounding box, so downstream
code can reconstruct tables, columns, reading order, or feed coordinates to an
LLM-vision pipeline.

## The Word type

```go
type Word struct {
	Text     string  // the word's text
	Page     int     // 0-based page index
	X0, Y0   float64 // lower-left corner
	X1, Y1   float64 // upper-right corner
	FontSize float64 // font size in points
}
```

## Coordinate system

Coordinates are in **PDF user space**, exactly as PDFium reports them:

- The origin `(0, 0)` is the **bottom-left** of the page.
- `X0, Y0` is the lower-left corner of the box; `X1, Y1` the upper-right.
- Units are **points** (1 pt = 1/72 inch). A US-Letter page is `612 × 792`.

Each `Page` also reports its own `Width` and `Height` in points, so you can
normalize boxes to `[0,1]` or flip the Y axis for top-left image coordinates:

```go
for _, pg := range doc.Pages {
	for _, w := range pg.Words {
		// top-left origin, normalized
		nx := w.X0 / pg.Width
		ny := (pg.Height - w.Y1) / pg.Height
		_ = nx
		_ = ny
	}
}
```

## How words are formed

PDFium reports text at the **character** level with per-glyph positions.
ParseRails groups consecutive non-whitespace characters into words and takes the
**union** of their character boxes as the word box. Whitespace and line breaks
end the current word.

This keeps extraction faithful to what is actually drawn on the page — no
heuristic reflow, no guessing.

> `FontSize` is `0` unless you opt in with `parserails.New(parserails.WithFontInfo())`.
> Collecting per-character font metrics roughly doubles extraction cost, so it is
> off by default.

## Granularity: word vs. line

Word-level grouping is precise but extracts every character across the Go↔WASM
boundary. When per-line boxes are enough (RAG chunking, full-text search), switch
to line granularity for a large speed and memory win:

```go
p, _ := parserails.New(parserails.WithGranularity(parserails.GranularityLine))
```

| Mode | Box precision | Relative cost |
|------|---------------|---------------|
| `GranularityWord` (default) | per word | baseline |
| `GranularityLine` | per line / text rect | ~6× faster, ~25× fewer allocs |

In line mode each `Word` spans a PDFium text rectangle (typically a line or run
fragment) rather than a single word. See the [Benchmarks](benchmarks.md) for
numbers.

## Working with pages

```go
doc, _ := p.Parse(ctx, pdf)

fmt.Println("pages:", len(doc.Pages))
for _, pg := range doc.Pages {
	fmt.Printf("page %d: %.0f×%.0f, %d words\n",
		pg.Index, pg.Width, pg.Height, len(pg.Words))
}
```

## Lines and reading order

PDFium reports characters and rectangles — nothing in the stream says where a
line ends. ParseRails reconstructs lines from the geometry: words are sorted by
their top edge, then grouped while they share enough of a vertical band, then
ordered left to right.

```go
for _, l := range doc.Lines() {
	fmt.Printf("p%d %q  [%.0f %.0f %.0f %.0f]\n", l.Page, l.Text(), l.X0, l.Y0, l.X1, l.Y1)
}
```

| Method | Returns |
|--------|---------|
| `doc.Lines()` | every page's lines, in page order |
| `page.Lines()` | one page's lines |
| `line.Text()` | the line's words joined with single spaces |
| `line.Height()` | the line's vertical extent in points |
| `line.FontSize()` | the line's dominant font size (0 without `WithFontInfo`) |

## Plain text output

`Document.Text()` is built on those lines: lines are joined with `\n` and pages
separated by a form feed (`\f`).

```go
text := doc.Text() // "First line\nSecond line\fPage two"
```

Reading order is top-to-bottom, then left-to-right, across the **whole page
width**. A two-column page therefore reads line by line across both columns —
use [Markdown output](markdown.md) when column structure matters, or
`ExtractText` when you want PDFium's own text order without any reconstruction.

## Read options: passwords and page ranges

Every in-memory entry point takes a `ReadOptions`, so one `Parser` can serve
documents with different passwords and page selections concurrently.

```go
doc, err := p.ParseData(ctx, data, parserails.ReadOptions{
	Name:     "statement.pdf", // format hint, only used when the bytes are ambiguous
	Password: "hunter2",       // encrypted documents
	Pages:    "1-5,10",        // 1-based, inclusive, in the order written
	MaxPages: 20,              // cap applied after Pages
})
```

| Field | Meaning |
|-------|---------|
| `Name` | file name hint for [format detection](formats.md) |
| `Password` | opens an encrypted document; defaults to `WithPassword` |
| `Pages` | 1-based selection like `"1-5,10,15-20"`; empty means all pages |
| `MaxPages` | caps pages read after `Pages`; defaults to `WithMaxPages`, negative means no cap |

Pages past the end of the document are skipped rather than rejected, so
`"1-10"` on a 3-page file returns those 3 pages. Selected pages keep their
**absolute** `Page.Index`, so `doc.Pages[0].Index == 1` after asking for page 2.

Parser-wide defaults:

```go
p, _ := parserails.New(
	parserails.WithPassword("hunter2"),
	parserails.WithMaxPages(50),
)
```

An encrypted document opened without a password fails with an error that says
so (`document is encrypted`), and a wrong password says `wrong password` —
rather than surfacing PDFium's numeric code. `RenderPage` takes the password
too, via `RenderRequest.Password`.
