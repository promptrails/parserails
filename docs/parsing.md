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

## Working with pages

```go
doc, _ := p.Parse(ctx, pdf)

fmt.Println("pages:", len(doc.Pages))
for _, pg := range doc.Pages {
	fmt.Printf("page %d: %.0f×%.0f, %d words\n",
		pg.Index, pg.Width, pg.Height, len(pg.Words))
}
```
