package parserails

import (
	"sort"
	"strings"
)

// Line is a horizontal run of words that share a baseline band on one page.
//
// PDFium reports text as characters or rectangles, not as lines: nothing in the
// extracted stream says where one line ends and the next begins. Line grouping
// reconstructs that from the geometry, which is what makes text output readable
// and what every higher-level structure (paragraphs, tables, Markdown) is built
// on.
type Line struct {
	Page           int     `json:"page"`
	X0, Y0, X1, Y1 float64 `json:"-"`
	Words          []Word  `json:"words"`
}

// Text joins the line's words with single spaces, in left-to-right order.
func (l Line) Text() string {
	parts := make([]string, 0, len(l.Words))
	for _, w := range l.Words {
		parts = append(parts, w.Text)
	}
	return strings.Join(parts, " ")
}

// Height reports the line's vertical extent in points.
func (l Line) Height() float64 { return l.Y1 - l.Y0 }

// FontSize reports the most common font size among the line's words, or 0 when
// the parser did not collect font information.
func (l Line) FontSize() float64 {
	counts := make(map[float64]int, 4)
	best, bestN := 0.0, 0
	for _, w := range l.Words {
		if w.FontSize == 0 {
			continue
		}
		counts[w.FontSize]++
		if n := counts[w.FontSize]; n > bestN || (n == bestN && w.FontSize > best) {
			best, bestN = w.FontSize, n
		}
	}
	return best
}

// Lines groups the page's words into lines in reading order (top to bottom,
// then left to right).
func (p Page) Lines() []Line { return groupLines(p.Words) }

// Text returns the page's text with its line breaks reconstructed.
func (p Page) Text() string {
	lines := p.Lines()
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		parts = append(parts, l.Text())
	}
	return strings.Join(parts, "\n")
}

// Lines groups every page's words into lines, in page order.
func (d *Document) Lines() []Line {
	var out []Line
	for i := range d.Pages {
		out = append(out, d.Pages[i].Lines()...)
	}
	return out
}

// verticalOverlapRatio is how much of the shorter box's height must be shared
// with the line band for a word to belong to that line. Superscripts and
// inline size changes overlap their line generously; the next line down does
// not.
const verticalOverlapRatio = 0.4

// groupLines reconstructs lines from loose words by their vertical overlap.
//
// Words arrive in extraction order, which follows the PDF content stream and
// need not be reading order at all. Sorting by descending top edge and sweeping
// once keeps this O(n log n): a word joins the open line when the two share
// enough vertical extent, and starts a new line otherwise.
func groupLines(words []Word) []Line {
	if len(words) == 0 {
		return nil
	}

	ws := make([]Word, len(words))
	copy(ws, words)
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].Page != ws[j].Page {
			return ws[i].Page < ws[j].Page
		}
		if ws[i].Y1 != ws[j].Y1 {
			return ws[i].Y1 > ws[j].Y1 // top of the page first
		}
		return ws[i].X0 < ws[j].X0
	})

	var (
		out  []Line
		cur  Line
		open bool
	)
	flush := func() {
		if !open {
			return
		}
		sort.SliceStable(cur.Words, func(i, j int) bool { return cur.Words[i].X0 < cur.Words[j].X0 })
		out = append(out, cur)
		open = false
	}
	for _, w := range ws {
		if open && w.Page == cur.Page && sharesBand(cur, w) {
			cur.X0 = min(cur.X0, w.X0)
			cur.Y0 = min(cur.Y0, w.Y0)
			cur.X1 = max(cur.X1, w.X1)
			cur.Y1 = max(cur.Y1, w.Y1)
			cur.Words = append(cur.Words, w)
			continue
		}
		flush()
		cur = Line{Page: w.Page, X0: w.X0, Y0: w.Y0, X1: w.X1, Y1: w.Y1, Words: []Word{w}}
		open = true
	}
	flush()
	return out
}

// sharesBand reports whether a word overlaps the open line's vertical band
// enough to belong to it.
func sharesBand(l Line, w Word) bool {
	overlap := min(l.Y1, w.Y1) - max(l.Y0, w.Y0)
	if overlap <= 0 {
		return false
	}
	shorter := min(l.Y1-l.Y0, w.Y1-w.Y0)
	if shorter <= 0 {
		return true // degenerate box: any overlap is all it has.
	}
	return overlap/shorter >= verticalOverlapRatio
}
