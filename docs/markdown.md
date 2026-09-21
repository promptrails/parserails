# Blocks & Markdown Output

Spatial words are the truth of a page, but LLM and RAG pipelines want
**structure**: what is a heading, what is a table, where a paragraph ends.
ParseRails reconstructs that from the geometry it already has.

```go
md := doc.Markdown()
fmt.Println(md)
```

```bash
parserails parse --format markdown report.pdf > report.md
```

Reconstruction is rule-based — no model, no network, no layout engine — so it
is fast and deterministic. Complex documents render imperfectly; dense tables
and unusual layouts are where it frays.

## Blocks

Markdown is rendered from a block decomposition you can take as data instead —
`Blocks()` with the defaults, `BlocksWith(BlockOptions{…})` to tune it:

```go
for _, b := range doc.Blocks() {
	fmt.Printf("%-10s p%d %q\n", b.Kind, b.Page, b.Text)
}
```

```go
type Block struct {
	Kind           BlockKind // heading | paragraph | list_item | table | figure
	Page           int
	X0, Y0, X1, Y1 float64

	Text    string   // heading, paragraph, list item
	Level   int      // heading depth, 1-6
	Ordered bool     // list item
	Marker  string   // the bullet or number as written
	Header  []Cell   // table
	Rows    [][]Cell
	ID      string   // figure, e.g. "img_p1_2"
}
```

| Kind | Carries |
|------|---------|
| `heading` | `Text`, `Level` (1–6) |
| `paragraph` | `Text`, with wrapped lines joined and hyphenated breaks healed |
| `list_item` | `Text`, `Marker`, `Ordered` |
| `table` | `Header`, `Rows` — each cell with its own box |
| `figure` | `ID`, e.g. `img_p1_2` |

Every block carries the page and the box it occupies (`X0,Y0,X1,Y1`), the union
of the lines that fed it, so a heading or a single table cell can be mapped
back to the region of the page it was read from — visual citations, highlight
overlays, review UIs.

## How each kind is decided

**Reading order.** Lines are grouped across the full page width, so on a
two-column page one "line" holds both columns. Columns are found as an empty
vertical channel with prose on both sides, and those lines are cut at it.
Full-width lines — a headline, a wide table — divide the page into bands, so a
single-column headline above two columns reads in the right order. A stack of
short entries beside another stack is a table, not a column, and is left
alone.

**Headings** are lines noticeably larger than the document's body text
(≥ 1.15×), short, and not ending in a full stop. Depth follows the size ratio.
Font sizes are used when `WithFontInfo` collected them and line heights stand
in when it did not, so headings work either way — the CLI turns font metrics
on automatically for Markdown.

**Paragraphs** merge consecutive lines with a normal gap, a matching left edge
and the same text size. A line ending in `-` is rejoined without it.

**List items** start with a bullet (`•`, `-`, `*`, …) or an enumerator
(`1.`, `2)`, `a.`). Wrapped continuation lines are folded in by their hanging
indent.

A table cell that held several paragraphs keeps the break as `<br>`: a pipe
table cannot contain a newline, and joining the paragraphs without one would
invent words that are in no document.

**Tables** are borderless-first: PDFium reports no ruling lines, so a table is
several consecutive lines whose words line up in the same columns. Cells are
split where the gap between words is far wider than the text size, and column
anchors are shared across rows so ragged rows still line up. A first row of
complete cells becomes the header.

**Figures** need `WithImages` (or `WithImageOCR`), which records each page's
raster objects. They render as `![](img_pN_K.png)` placeholders, slotted into
the block sequence by vertical position — the text blocks themselves are never
reordered, so columns stay in reading order and a page selection stays in the
order it was asked for.

## Running headers and footers

A line that repeats at the top or bottom of most pages is furniture, not
content, and is dropped — including page numbers, which are matched with
digits normalized so "Page 3 of 12" and "Page 4 of 12" count as the same line.

```go
md := doc.MarkdownWith(parserails.BlockOptions{KeepHeadersFooters: true})
```

```bash
parserails parse --format markdown --keep-headers-footers report.pdf
```
