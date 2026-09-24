package parserails

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

// BlockKind classifies a region of a page.
type BlockKind string

// The block kinds ParseRails reconstructs.
const (
	BlockHeading   BlockKind = "heading"
	BlockParagraph BlockKind = "paragraph"
	BlockListItem  BlockKind = "list_item"
	BlockTable     BlockKind = "table"
	BlockFigure    BlockKind = "figure"
)

// Cell is one table cell with the region it was read from.
type Cell struct {
	Text           string  `json:"text"`
	X0, Y0, X1, Y1 float64 `json:"-"`
}

// Block is a classified region of a page, in reading order.
//
// Blocks are what [Document.Markdown] output is built from. Taking them as data
// of as rendered text gives you the same decomposition with the coordinates
// the classifier used, so a heading or a table cell can be mapped back to the
// part of the page it came from.
type Block struct {
	Kind           BlockKind `json:"kind"`
	Page           int       `json:"page"`
	X0, Y0, X1, Y1 float64   `json:"-"`

	Text  string `json:"text,omitempty"`  // heading, paragraph, list item
	Level int    `json:"level,omitempty"` // heading depth, 1-6

	Ordered bool   `json:"ordered,omitempty"` // list item
	Marker  string `json:"marker,omitempty"`  // the bullet or number as written

	Header []Cell   `json:"header,omitempty"` // table
	Rows   [][]Cell `json:"rows,omitempty"`

	ID string `json:"id,omitempty"` // figure, e.g. "img_p1_2"
}

// BlockOptions tunes block reconstruction.
type BlockOptions struct {
	// KeepHeadersFooters keeps running headers and footers. By default a line
	// that repeats at the top or bottom of most pages is dropped, since it is
	// furniture rather than content.
	KeepHeadersFooters bool
}

// Heuristic constants. Layout reconstruction is rule-based: these are the
// rules. They are tuned for body text on office-sized pages.
const (
	headingSizeRatio      = 1.15 // a heading is this much larger than body text
	headingMaxWords       = 14
	paragraphGapFactor    = 1.8  // line gap up to this many line heights stays in the paragraph
	indentTolerance       = 14.0 // points; left edges this close count as aligned
	columnGapMin          = 18.0 // points; a word gap this wide separates table columns
	columnGapFactor       = 1.5  // ...or this many times the line's text size
	columnAnchorTol       = 12.0 // points; cell starts this close share a column
	gutterMin             = 24.0 // points; a vertical gap this wide can split columns
	fullWidthShare        = 0.7  // a line this wide, with no hole, divides the page
	minColumnLines        = 4    // fewer lines than this is not a column of prose
	minColumnWordsPerLine = 3.0
	gutterBucket          = 4.0  // points; resolution of the gutter scan
	edgeZone              = 0.12 // fraction of page height that counts as header/footer
	repeatShare           = 0.6  // a line repeating on this share of pages is furniture
)

// Blocks reconstructs the document's structure with the default options.
func (d *Document) Blocks() []Block { return d.BlocksWith(BlockOptions{}) }

// BlocksWith reconstructs the document's structure: headings, paragraphs, list
// items, tables and figures, in reading order.
//
// Nothing in a PDF says "this is a heading". The classifier works from the
// geometry ParseRails already has — line boxes, font sizes, gaps and
// alignment — so it is fast and deterministic, and wrong on documents whose
// layout does not follow the usual conventions. Dense tables and unusual
// layouts are where it frays.
func (d *Document) BlocksWith(opt BlockOptions) []Block {
	furniture := map[string]bool{}
	if !opt.KeepHeadersFooters {
		furniture = repeatedEdgeLines(d)
	}

	var out []Block
	for i := range d.Pages {
		page := d.Pages[i]
		lines := page.Lines()
		if !opt.KeepHeadersFooters {
			lines = dropFurniture(lines, page, furniture)
		}
		body := bodyTextSize(lines)
		var text []Block
		for _, group := range readingOrder(lines, page) {
			text = append(text, classify(group, page, body)...)
		}
		out = append(out, placeFigures(text, figureBlocks(page))...)
	}
	return out
}

// placeFigures slots a page's figures into its block sequence by vertical
// position, without reordering the blocks themselves.
//
// Sorting the whole page by position instead would undo the reading order
// that was just established: on a two-column page it interleaves the columns
// again, and across pages it re-sorts a selection the caller asked for in
// another order.
func placeFigures(blocks, figures []Block) []Block {
	if len(figures) == 0 {
		return blocks
	}
	sort.SliceStable(figures, func(i, j int) bool { return figures[i].Y1 > figures[j].Y1 })

	out := make([]Block, 0, len(blocks)+len(figures))
	next := 0
	for _, b := range blocks {
		for next < len(figures) && figures[next].Y1 > b.Y1 {
			out = append(out, figures[next])
			next++
		}
		out = append(out, b)
	}
	return append(out, figures[next:]...)
}

func figureBlocks(page Page) []Block {
	out := make([]Block, 0, len(page.Images))
	for _, img := range page.Images {
		out = append(out, Block{
			Kind: BlockFigure, Page: page.Index,
			X0: img.X0, Y0: img.Y0, X1: img.X1, Y1: img.Y1,
			ID: fmt.Sprintf("img_p%d_%d", page.Index+1, img.Index+1),
		})
	}
	return out
}

// repeatedEdgeLines finds the lines that repeat at the top or bottom of most
// pages — running headers, footers, page numbers.
func repeatedEdgeLines(d *Document) map[string]bool {
	if len(d.Pages) < 3 {
		return nil
	}
	seen := make(map[string]map[int]bool)
	for i := range d.Pages {
		page := d.Pages[i]
		for _, l := range page.Lines() {
			if !inEdgeZone(l, page) {
				continue
			}
			key := furnitureKey(l.Text())
			if key == "" {
				continue
			}
			if seen[key] == nil {
				seen[key] = map[int]bool{}
			}
			seen[key][page.Index] = true
		}
	}
	out := map[string]bool{}
	need := int(math.Ceil(repeatShare * float64(len(d.Pages))))
	for key, pages := range seen {
		if len(pages) >= need {
			out[key] = true
		}
	}
	return out
}

func dropFurniture(lines []Line, page Page, furniture map[string]bool) []Line {
	if len(furniture) == 0 {
		return lines
	}
	out := lines[:0:0]
	for _, l := range lines {
		if inEdgeZone(l, page) && furniture[furnitureKey(l.Text())] {
			continue
		}
		out = append(out, l)
	}
	return out
}

func inEdgeZone(l Line, page Page) bool {
	if page.Height <= 0 {
		return false
	}
	zone := page.Height * edgeZone
	return l.Y0 >= page.Height-zone || l.Y1 <= zone
}

// furnitureKey normalizes a line so "Page 3 of 12" and "Page 4 of 12" collapse
// onto the same running footer.
func furnitureKey(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsDigit(r):
			b.WriteByte('#')
		case unicode.IsSpace(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// bodyTextSize is the document's ordinary text size, used as the yardstick for
// headings. Font sizes are used when they were collected; line heights stand
// in for them when they were not, which is close enough for a ratio.
func bodyTextSize(lines []Line) float64 {
	sizes := make([]float64, 0, len(lines))
	for _, l := range lines {
		if s := lineSize(l); s > 0 {
			for range l.Words { // weight by text, not by line count
				sizes = append(sizes, s)
			}
		}
	}
	if len(sizes) == 0 {
		return 0
	}
	sort.Float64s(sizes)
	return sizes[len(sizes)/2]
}

func lineSize(l Line) float64 {
	if s := l.FontSize(); s > 0 {
		return s
	}
	return l.Height()
}

// readingOrder splits a page into regions that read independently — the
// columns of a multi-column page — and returns each region's lines in order.
//
// Line grouping works across the full page width, so on a two-column page one
// "line" holds the left column's line and the right column's line. Columns are
// therefore found first and those lines are cut at the gutter, and full-width
// lines (a headline, a wide table) divide the page into bands so a
// single-column headline above two columns reads the way a person reads it.
func readingOrder(lines []Line, page Page) [][]Line {
	if regions, ok := columnRegions(lines); ok {
		return regions
	}
	return [][]Line{lines}
}

// columnRegions bands the page at its full-width lines and splits each band at
// its gutter. It reports false when no band actually turned out to be laid out
// in columns, so a single-column page is never fragmented by its own
// full-width body lines.
func columnRegions(lines []Line) ([][]Line, bool) {
	if len(lines) < 2 {
		return nil, false
	}
	textLeft, textRight := textExtent(lines)

	var (
		out   [][]Line
		band  []Line
		split bool
	)
	flush := func() {
		if len(band) == 0 {
			return
		}
		columns := splitColumns(band, textLeft, textRight)
		if len(columns) > 1 {
			split = true
		}
		out = append(out, columns...)
		band = nil
	}
	for _, l := range lines {
		if isFullWidth(l, textLeft, textRight) {
			flush()
			out = append(out, []Line{l})
			continue
		}
		band = append(band, l)
	}
	flush()
	return out, split
}

// textExtent is the horizontal span the page's text actually occupies, which
// is what "full width" and "the middle of the page" mean here — margins vary.
func textExtent(lines []Line) (left, right float64) {
	left, right = math.Inf(1), math.Inf(-1)
	for _, l := range lines {
		left, right = min(left, l.X0), max(right, l.X1)
	}
	return left, right
}

// isFullWidth reports whether a line runs the width of the text block without
// a hole in it. A line that merely looks wide because it holds two columns'
// worth of text has a gap in the middle, and is not one of these.
func isFullWidth(l Line, textLeft, textRight float64) bool {
	span := textRight - textLeft
	if span <= 0 || l.X1-l.X0 < span*fullWidthShare {
		return false
	}
	for i := 1; i < len(l.Words); i++ {
		if l.Words[i].X0-l.Words[i-1].X1 >= gutterMin {
			return false
		}
	}
	return true
}

// splitColumns cuts a band of lines at its gutter, returning one region per
// column, or the band unchanged when it is not laid out in columns.
func splitColumns(band []Line, textLeft, textRight float64) [][]Line {
	g, ok := findGutter(band, textLeft, textRight)
	if !ok {
		return [][]Line{band}
	}
	var left, right []Line
	for _, l := range band {
		l, r, ok := splitLineAt(l, g)
		if !ok {
			return [][]Line{band} // a word sits in the gutter; not columns after all
		}
		if len(l.Words) > 0 {
			left = append(left, l)
		}
		if len(r.Words) > 0 {
			right = append(right, r)
		}
	}
	if !readsAsColumn(left) || !readsAsColumn(right) {
		return [][]Line{band}
	}
	return [][]Line{left, right}
}

// readsAsColumn distinguishes a column of prose from the side of a table.
// Both are stacks of text with a gap down the middle; a column has enough
// lines, and enough words per line, to be reading matter.
func readsAsColumn(lines []Line) bool {
	if len(lines) < minColumnLines {
		return false
	}
	words := 0
	for _, l := range lines {
		words += len(l.Words)
	}
	return float64(words)/float64(len(lines)) >= minColumnWordsPerLine
}

// gutter is a vertical band of a page that no word occupies.
type gutter struct{ start, end float64 }

// splitLineAt cuts a line into its part left of the gutter and its part right
// of it. It fails if a word straddles the gutter, which means this is not a
// gutter for this line.
func splitLineAt(l Line, g gutter) (left, right Line, ok bool) {
	left = Line{Page: l.Page}
	right = Line{Page: l.Page}
	for _, w := range l.Words {
		switch {
		case w.X1 <= g.start+columnAnchorTol:
			left.Words = append(left.Words, w)
		case w.X0 >= g.end-columnAnchorTol:
			right.Words = append(right.Words, w)
		default:
			return Line{}, Line{}, false
		}
	}
	return boundLine(left), boundLine(right), true
}

func boundLine(l Line) Line {
	if len(l.Words) == 0 {
		return l
	}
	l.X0, l.Y0 = l.Words[0].X0, l.Words[0].Y0
	l.X1, l.Y1 = l.Words[0].X1, l.Words[0].Y1
	for _, w := range l.Words[1:] {
		l.X0, l.Y0 = min(l.X0, w.X0), min(l.Y0, w.Y0)
		l.X1, l.Y1 = max(l.X1, w.X1), max(l.Y1, w.Y1)
	}
	return l
}

// findGutter looks for the widest channel, in the middle of the text block,
// that no word occupies.
func findGutter(lines []Line, textLeft, textRight float64) (gutter, bool) {
	span := textRight - textLeft
	if span <= 0 {
		return gutter{}, false
	}
	buckets := int(span/gutterBucket) + 1
	inked := make([]bool, buckets)
	for _, l := range lines {
		for _, w := range l.Words {
			from := int((w.X0 - textLeft) / gutterBucket)
			to := int((w.X1 - textLeft) / gutterBucket)
			for i := from; i <= to && i < buckets; i++ {
				if i >= 0 {
					inked[i] = true
				}
			}
		}
	}

	// Only the middle can hold a gutter; the margins are not columns.
	lo, hi := int(float64(buckets)*0.2), int(float64(buckets)*0.8)
	best, bestWidth := gutter{}, 0.0
	for i := lo; i <= hi && i < buckets; i++ {
		if inked[i] {
			continue
		}
		j := i
		for j+1 < buckets && j+1 <= hi && !inked[j+1] {
			j++
		}
		if width := float64(j-i+1) * gutterBucket; width > bestWidth {
			bestWidth = width
			best = gutter{
				start: textLeft + float64(i)*gutterBucket,
				end:   textLeft + float64(j+1)*gutterBucket,
			}
		}
		i = j
	}
	if bestWidth < gutterMin {
		return gutter{}, false
	}
	return best, true
}

// classify turns one region's lines into blocks.
func classify(lines []Line, page Page, bodySize float64) []Block {
	var out []Block
	for i := 0; i < len(lines); {
		if table, used := tableAt(lines, i, page); used > 0 {
			out = append(out, table)
			i += used
			continue
		}
		line := lines[i]
		if marker, rest, ordered, ok := listMarker(line.Text()); ok {
			block := blockFrom(BlockListItem, line, page)
			block.Marker, block.Text, block.Ordered = marker, rest, ordered
			i++
			// Continuation lines of a wrapped list item are indented under it.
			for i < len(lines) && continuesItem(lines[i-1], lines[i], line.X0) {
				block.Text = joinWrapped(block.Text, lines[i].Text())
				block = growBlock(block, lines[i])
				i++
			}
			out = append(out, block)
			continue
		}
		if level, ok := headingLevel(line, bodySize); ok {
			block := blockFrom(BlockHeading, line, page)
			block.Text, block.Level = line.Text(), level
			out = append(out, block)
			i++
			continue
		}

		block := blockFrom(BlockParagraph, line, page)
		block.Text = line.Text()
		i++
		for i < len(lines) && continuesParagraph(lines[i-1], lines[i]) && !startsBlock(lines[i], bodySize) {
			block.Text = joinWrapped(block.Text, lines[i].Text())
			block = growBlock(block, lines[i])
			i++
		}
		out = append(out, block)
	}
	return out
}

func blockFrom(kind BlockKind, l Line, page Page) Block {
	return Block{Kind: kind, Page: page.Index, X0: l.X0, Y0: l.Y0, X1: l.X1, Y1: l.Y1}
}

func growBlock(b Block, l Line) Block {
	b.X0, b.Y0 = min(b.X0, l.X0), min(b.Y0, l.Y0)
	b.X1, b.Y1 = max(b.X1, l.X1), max(b.Y1, l.Y1)
	return b
}

// joinWrapped appends a wrapped line, healing words broken across the break.
func joinWrapped(text, next string) string {
	if strings.HasSuffix(text, "-") && next != "" && unicode.IsLower(rune(next[0])) {
		return strings.TrimSuffix(text, "-") + next
	}
	return text + " " + next
}

func continuesParagraph(prev, next Line) bool {
	gap := prev.Y0 - next.Y1
	if gap < 0 || gap > prev.Height()*paragraphGapFactor {
		return false
	}
	if math.Abs(prev.X0-next.X0) > indentTolerance && next.X0 < prev.X0 {
		return false // a new, less-indented line starts something else
	}
	// A size change means a heading or a caption, not a continuation.
	return math.Abs(lineSize(prev)-lineSize(next)) <= 0.5
}

// startsBlock reports whether a line opens something of its own — a list
// item, a heading — rather than continuing the text above it. Spacing alone
// cannot tell: a normally spaced list under an introductory sentence looks
// exactly like the next line of that sentence.
func startsBlock(l Line, bodySize float64) bool {
	if _, _, _, ok := listMarker(l.Text()); ok {
		return true
	}
	_, isHeading := headingLevel(l, bodySize)
	return isHeading
}

func continuesItem(prev, next Line, itemLeft float64) bool {
	if !continuesParagraph(prev, next) {
		return false
	}
	if _, _, _, isItem := listMarker(next.Text()); isItem {
		return false
	}
	return next.X0 > itemLeft // hanging indent
}

// listMarker splits a leading bullet or number off a line.
func listMarker(text string) (marker, rest string, ordered, ok bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", "", false, false
	}
	for _, bullet := range []string{"•", "◦", "▪", "·", "–", "—", "-", "*"} {
		if after, found := strings.CutPrefix(trimmed, bullet+" "); found {
			return bullet, strings.TrimSpace(after), false, true
		}
	}
	// "1." / "2)" / "a." — a number or letter followed by a separator.
	for i, r := range trimmed {
		if r == '.' || r == ')' {
			head := trimmed[:i]
			if i == 0 || i > 3 || !isEnumeration(head) {
				return "", "", false, false
			}
			rest := strings.TrimSpace(trimmed[i+1:])
			if rest == "" {
				return "", "", false, false
			}
			return trimmed[:i+1], rest, true, true
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
	}
	return "", "", false, false
}

func isEnumeration(s string) bool {
	if s == "" {
		return false
	}
	digits, letters := 0, 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
		default:
			return false
		}
	}
	return digits == len(s) || (letters == len(s) && len(s) == 1)
}

// headingLevel decides whether a line is a heading, and how deep.
func headingLevel(l Line, bodySize float64) (int, bool) {
	text := strings.TrimSpace(l.Text())
	if text == "" || len(l.Words) > headingMaxWords || strings.HasSuffix(text, ".") {
		return 0, false
	}
	size := lineSize(l)
	if bodySize <= 0 || size <= 0 {
		return 0, false
	}
	ratio := size / bodySize
	switch {
	case ratio >= 1.8:
		return 1, true
	case ratio >= 1.45:
		return 2, true
	case ratio >= 1.25:
		return 3, true
	case ratio >= headingSizeRatio:
		return 4, true
	}
	return 0, false
}

// tableAt tries to read a borderless table starting at lines[i], returning the
// block and how many lines it consumed.
//
// Ruled tables are drawn with vector lines PDFium does not report as text, so
// detection works the way a reader does: several consecutive lines whose words
// line up in the same columns.
func tableAt(lines []Line, i int, page Page) (Block, int) {
	var (
		rows    [][]Cell
		anchors []float64
		used    int
	)
	for j := i; j < len(lines); j++ {
		cells := splitCells(lines[j])
		if len(cells) < 2 {
			break
		}
		if j > i && !sameColumns(anchors, cells) {
			break
		}
		if j > i && lines[j-1].Y0-lines[j].Y1 > lines[j-1].Height()*paragraphGapFactor {
			break // too far below the previous row to be part of it
		}
		anchors = mergeAnchors(anchors, cells)
		rows = append(rows, cells)
		used++
	}
	if used < 2 || len(anchors) < 2 {
		return Block{}, 0
	}

	block := blockFrom(BlockTable, lines[i], page)
	for j := i; j < i+used; j++ {
		block = growBlock(block, lines[j])
	}
	block.Rows = alignRows(rows, anchors)
	// A first row of complete, distinct cells reads as a header.
	if len(block.Rows) > 1 && complete(block.Rows[0]) {
		block.Header, block.Rows = block.Rows[0], block.Rows[1:]
	}
	return block, used
}

// splitCells breaks a line into cells wherever the gap between words is far
// wider than the space between words of that size.
//
// The threshold is derived from the text size, not from the gaps themselves:
// on a table row every gap is a column gap, so measuring the gaps would raise
// the bar above them and find nothing.
func splitCells(l Line) []Cell {
	if len(l.Words) == 0 {
		return nil
	}
	threshold := max(columnGapMin, lineSize(l)*columnGapFactor)

	var (
		cells []Cell
		cur   = cellFrom(l.Words[0])
	)
	for i := 1; i < len(l.Words); i++ {
		w := l.Words[i]
		if w.X0-l.Words[i-1].X1 >= threshold {
			cells = append(cells, cur)
			cur = cellFrom(w)
			continue
		}
		cur.Text += " " + w.Text
		cur.X1, cur.Y0, cur.Y1 = w.X1, min(cur.Y0, w.Y0), max(cur.Y1, w.Y1)
	}
	return append(cells, cur)
}

func cellFrom(w Word) Cell {
	return Cell{Text: w.Text, X0: w.X0, Y0: w.Y0, X1: w.X1, Y1: w.Y1}
}

func mergeAnchors(anchors []float64, cells []Cell) []float64 {
	for _, c := range cells {
		if !nearAnchor(anchors, c.X0) {
			anchors = append(anchors, c.X0)
		}
	}
	sort.Float64s(anchors)
	return anchors
}

func nearAnchor(anchors []float64, x float64) bool {
	for _, a := range anchors {
		if math.Abs(a-x) <= columnAnchorTol {
			return true
		}
	}
	return false
}

// sameColumns reports whether a row's cells line up with the columns seen so
// far. One stray cell is allowed — a wrapped value, a merged heading.
func sameColumns(anchors []float64, cells []Cell) bool {
	if len(anchors) == 0 {
		return true
	}
	off := 0
	for _, c := range cells {
		if !nearAnchor(anchors, c.X0) {
			off++
		}
	}
	return off <= 1
}

// alignRows places every row's cells under the column they start in, padding
// the gaps so the table is rectangular.
//
// Two cells of one row can resolve to the same column when the anchors came
// from a differently spaced row. They are merged rather than overwritten:
// a slightly wrong table beats a table with text silently missing from it.
func alignRows(rows [][]Cell, anchors []float64) [][]Cell {
	out := make([][]Cell, 0, len(rows))
	for _, row := range rows {
		aligned := make([]Cell, len(anchors))
		for _, c := range row {
			at := nearestAnchor(anchors, c.X0)
			if existing := aligned[at]; existing.Text != "" {
				c.Text = existing.Text + " " + c.Text
				c.X0, c.Y0 = min(existing.X0, c.X0), min(existing.Y0, c.Y0)
				c.X1, c.Y1 = max(existing.X1, c.X1), max(existing.Y1, c.Y1)
			}
			aligned[at] = c
		}
		out = append(out, aligned)
	}
	return out
}

func nearestAnchor(anchors []float64, x float64) int {
	best, bestDist := 0, math.Inf(1)
	for i, a := range anchors {
		if d := math.Abs(a - x); d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

func complete(row []Cell) bool {
	for _, c := range row {
		if strings.TrimSpace(c.Text) == "" {
			return false
		}
	}
	return len(row) > 0
}
