package parserails

import (
	"context"
	"strings"
	"testing"
)

// parseRuns parses a one-page document built from the given runs, with font
// sizes collected so heading detection has real sizes to work with.
func parseRuns(t *testing.T, runs ...textRun) *Document {
	t.Helper()
	p := newTestParser(t, WithFontInfo())
	doc, err := p.Parse(context.Background(), pdfWithRuns(runs...))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func TestBlocksClassifiesHeadingsParagraphsAndLists(t *testing.T) {
	doc := parseRuns(t,
		textRun{Text: "Quarterly Report", X: 72, Y: 720, Size: 24},
		textRun{Text: "Revenue grew across every region this quarter, driven mostly by", X: 72, Y: 690, Size: 11},
		textRun{Text: "renewals in the enterprise segment.", X: 72, Y: 676, Size: 11},
		textRun{Text: "Highlights", X: 72, Y: 640, Size: 16},
		textRun{Text: "- Renewals up 12 percent", X: 72, Y: 620, Size: 11},
		textRun{Text: "- Churn down to 4 percent", X: 72, Y: 606, Size: 11},
	)

	blocks := doc.Blocks()
	var kinds []string
	for _, b := range blocks {
		kinds = append(kinds, string(b.Kind))
	}
	want := "heading paragraph heading list_item list_item"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("kinds = %q, want %q\nblocks: %+v", got, want, blocks)
	}

	if blocks[0].Level >= blocks[2].Level {
		t.Errorf("the 24pt title should outrank the 16pt heading: %d vs %d",
			blocks[0].Level, blocks[2].Level)
	}
	// The wrapped sentence is one paragraph, not two.
	if got := blocks[1].Text; !strings.Contains(got, "driven mostly by renewals") {
		t.Errorf("paragraph = %q, want the wrapped lines joined", got)
	}
	if blocks[3].Text != "Renewals up 12 percent" || blocks[3].Ordered {
		t.Errorf("list item = %+v, want an unordered item without its bullet", blocks[3])
	}
}

func TestBlocksReadsBorderlessTables(t *testing.T) {
	doc := parseRuns(t,
		textRun{Text: "Region", X: 72, Y: 700, Size: 11},
		textRun{Text: "Revenue", X: 220, Y: 700, Size: 11},
		textRun{Text: "Growth", X: 380, Y: 700, Size: 11},
		textRun{Text: "EMEA", X: 72, Y: 684, Size: 11},
		textRun{Text: "1.2M", X: 220, Y: 684, Size: 11},
		textRun{Text: "12%", X: 380, Y: 684, Size: 11},
		textRun{Text: "APAC", X: 72, Y: 668, Size: 11},
		textRun{Text: "0.8M", X: 220, Y: 668, Size: 11},
		textRun{Text: "7%", X: 380, Y: 668, Size: 11},
	)

	blocks := doc.Blocks()
	if len(blocks) != 1 || blocks[0].Kind != BlockTable {
		t.Fatalf("blocks = %+v, want a single table", blocks)
	}
	table := blocks[0]
	if len(table.Header) != 3 || table.Header[0].Text != "Region" {
		t.Fatalf("header = %+v, want three columns starting with Region", table.Header)
	}
	if len(table.Rows) != 2 || table.Rows[1][2].Text != "7%" {
		t.Fatalf("rows = %+v", table.Rows)
	}
	// Cells keep the region they were read from.
	if diff := table.Rows[0][0].X0 - table.Header[0].X0; diff > 1 || diff < -1 {
		t.Errorf("column cells should share a left edge: %v vs %v",
			table.Rows[0][0].X0, table.Header[0].X0)
	}
}

func TestMarkdownRendersStructure(t *testing.T) {
	doc := parseRuns(t,
		textRun{Text: "Title", X: 72, Y: 720, Size: 24},
		textRun{Text: "A paragraph of body text that says something.", X: 72, Y: 690, Size: 11},
		textRun{Text: "1. First step", X: 72, Y: 660, Size: 11},
		textRun{Text: "2. Second step", X: 72, Y: 646, Size: 11},
		textRun{Text: "Region", X: 72, Y: 600, Size: 11},
		textRun{Text: "Revenue", X: 250, Y: 600, Size: 11},
		textRun{Text: "EMEA", X: 72, Y: 584, Size: 11},
		textRun{Text: "1.2M", X: 250, Y: 584, Size: 11},
	)

	md := doc.Markdown()
	want := []string{
		"# Title",
		"A paragraph of body text that says something.",
		"1. First step\n2. Second step",
		"| Region | Revenue |",
		"| --- | --- |",
		"| EMEA | 1.2M |",
	}
	for _, fragment := range want {
		if !strings.Contains(md, fragment) {
			t.Errorf("markdown missing %q:\n%s", fragment, md)
		}
	}
}

func TestMarkdownTableIsOneUnbrokenBlock(t *testing.T) {
	md := markdownTable(Block{
		Header: []Cell{{Text: "Region"}, {Text: "Revenue"}},
		Rows: [][]Cell{
			{{Text: "EMEA"}, {Text: "1.2M"}},
			{{Text: "APAC"}, {Text: "0.8M"}},
			{{Text: "AMER"}, {Text: "2.1M"}},
		},
	})
	if strings.Contains(md, "\n\n") {
		t.Fatalf("a blank line ends the table early:\n%q", md)
	}
	if lines := strings.Split(md, "\n"); len(lines) != 5 {
		t.Fatalf("got %d lines, want header + separator + three rows:\n%s", len(lines), md)
	}
}

func TestMarkdownTableDoesNotPadRaggedRows(t *testing.T) {
	// One wide row among many narrow ones. Padding every row out to the
	// widest rebuilds the rectangle a ragged table exists to avoid.
	wide := make([]Cell, 500)
	for i := range wide {
		wide[i] = Cell{Text: "x"}
	}
	rows := [][]Cell{wide}
	for i := 0; i < 500; i++ {
		rows = append(rows, []Cell{{Text: "y"}})
	}

	md := markdownTable(Block{Rows: rows})
	// 500 wide cells plus 500 narrow rows, not 500 x 500.
	if len(md) > 64<<10 {
		t.Fatalf("rendered %d KiB from 1000 cells", len(md)>>10)
	}
	lines := strings.Split(md, "\n")
	if got := strings.Count(lines[len(lines)-1], "|"); got != 2 {
		t.Errorf("last row has %d pipes, want a row of its own width", got)
	}
}

func TestBlocksDropsRunningHeadersAndFooters(t *testing.T) {
	p := newTestParser(t, WithFontInfo())
	var pages [][]textRun
	for i := 1; i <= 4; i++ {
		pages = append(pages, []textRun{
			{Text: "ACME Confidential", X: 72, Y: 750, Size: 9},
			{Text: "Body text for the page that carries the actual content.", X: 72, Y: 500, Size: 11},
			{Text: "Page " + string(rune('0'+i)) + " of 4", X: 72, Y: 40, Size: 9},
		})
	}
	doc, err := p.Parse(context.Background(), pdfWithPages(pages))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	md := doc.Markdown()
	if strings.Contains(md, "ACME Confidential") {
		t.Errorf("running header kept:\n%s", md)
	}
	if strings.Contains(md, "of 4") {
		t.Errorf("running footer kept:\n%s", md)
	}
	if !strings.Contains(md, "Body text for the page") {
		t.Errorf("body dropped:\n%s", md)
	}

	kept := doc.MarkdownWith(BlockOptions{KeepHeadersFooters: true})
	if !strings.Contains(kept, "ACME Confidential") {
		t.Errorf("KeepHeadersFooters should keep the header:\n%s", kept)
	}
}

func TestBlocksReadsColumnsInOrder(t *testing.T) {
	runs := []textRun{
		{Text: "A Headline Across Both Columns", X: 72, Y: 720, Size: 20},
	}
	// Two columns of prose, interleaved vertically: line grouping sees each
	// pair as one line, so the classifier has to cut them at the gutter.
	for i := 0; i < 6; i++ {
		y := 690 - float64(i)*16
		runs = append(runs,
			textRun{Text: leftColumnLine(i), X: 72, Y: y, Size: 11},
			textRun{Text: rightColumnLine(i), X: 330, Y: y, Size: 11},
		)
	}
	doc := parseRuns(t, runs...)

	var texts []string
	for _, b := range doc.Blocks() {
		texts = append(texts, b.Text)
	}
	joined := strings.Join(texts, " | ")
	lastLeft := strings.Index(joined, "left column line six")
	firstRight := strings.Index(joined, "right column line one")
	if lastLeft < 0 || firstRight < 0 {
		t.Fatalf("columns missing: %q", joined)
	}
	if lastLeft > firstRight {
		t.Errorf("columns interleaved instead of read in order: %q", joined)
	}
	if !strings.HasPrefix(joined, "A Headline Across Both Columns") {
		t.Errorf("the full-width headline should come first: %q", joined)
	}
}

var ordinals = []string{"one", "two", "three", "four", "five", "six"}

func leftColumnLine(i int) string  { return "left column line " + ordinals[i] + " of prose" }
func rightColumnLine(i int) string { return "right column line " + ordinals[i] + " of prose" }

func TestFigureBlocksArePlacedInReadingOrder(t *testing.T) {
	p := newTestParser(t, WithImages(), WithFontInfo())
	doc, err := p.Parse(context.Background(), pdfWithPageSpecs([]pdfPage{{
		Runs: []textRun{
			{Text: "Above the figure", X: 72, Y: 700, Size: 11},
			{Text: "Below the figure", X: 72, Y: 200, Size: 11},
		},
		Images: []imageBox{{X: 72, Y: 300, W: 400, H: 300}},
	}}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	md := doc.Markdown()
	above := strings.Index(md, "Above the figure")
	figure := strings.Index(md, "![](img_p1_1.png)")
	below := strings.Index(md, "Below the figure")
	if above < 0 || figure < 0 || below < 0 {
		t.Fatalf("markdown missing pieces:\n%s", md)
	}
	if above > figure || figure > below {
		t.Errorf("figure out of order:\n%s", md)
	}
}

func TestColumnsStayInOrderWhenEachHasSeveralBlocks(t *testing.T) {
	var runs []textRun
	for i := 0; i < 6; i++ {
		y := 690 - float64(i)*16
		if i >= 3 {
			y -= 30 // a paragraph break, so each column yields two blocks
		}
		runs = append(runs,
			textRun{Text: leftColumnLine(i), X: 72, Y: y, Size: 11},
			textRun{Text: rightColumnLine(i), X: 330, Y: y, Size: 11})
	}
	doc := parseRuns(t, runs...)

	md := doc.Markdown()
	if strings.Index(md, "right column line one") < strings.Index(md, "left column line six") {
		t.Fatalf("columns interleaved by vertical position:\n%s", md)
	}
}

func TestBlocksKeepTheRequestedPageOrder(t *testing.T) {
	p := newTestParser(t, WithFontInfo())
	pdf := pdfWithPages([][]textRun{
		{{Text: "Page one carries a sentence of text.", X: 72, Y: 700, Size: 11}},
		{{Text: "Page two carries a sentence of text.", X: 72, Y: 700, Size: 11}},
	})
	doc, err := p.ParseData(context.Background(), pdf, ReadOptions{Pages: "2,1"})
	if err != nil {
		t.Fatalf("ParseData: %v", err)
	}
	md := doc.Markdown()
	if strings.Index(md, "Page two") > strings.Index(md, "Page one") {
		t.Fatalf("pages were re-sorted; 2,1 was requested:\n%s", md)
	}
}

func TestParagraphStopsWhereAListBegins(t *testing.T) {
	doc := parseRuns(t,
		textRun{Text: "The quarter closed with these results:", X: 72, Y: 700, Size: 11},
		textRun{Text: "1. Renewals up twelve percent", X: 72, Y: 686, Size: 11},
		textRun{Text: "2. Churn down to four percent", X: 72, Y: 672, Size: 11},
	)
	var kinds []string
	for _, b := range doc.Blocks() {
		kinds = append(kinds, string(b.Kind))
	}
	if got := strings.Join(kinds, " "); got != "paragraph list_item list_item" {
		t.Fatalf("kinds = %q, want the list kept out of the paragraph", got)
	}
}

func TestAlignRowsMergesInsteadOfDropping(t *testing.T) {
	rows := [][]Cell{{{Text: "left", X0: 100}, {Text: "right", X0: 118}}}
	got := alignRows(rows, []float64{110})
	if len(got) != 1 || got[0][0].Text != "left right" {
		t.Fatalf("aligned = %+v, want both cells kept", got)
	}
}

func TestListMarker(t *testing.T) {
	cases := []struct {
		in      string
		marker  string
		rest    string
		ordered bool
		ok      bool
	}{
		{"• Item one", "•", "Item one", false, true},
		{"- Item two", "-", "Item two", false, true},
		{"1. First", "1.", "First", true, true},
		{"12) Twelfth", "12)", "Twelfth", true, true},
		{"a. Letter", "a.", "Letter", true, true},
		{"Not a list at all", "", "", false, false},
		{"2025. was a year", "", "", false, false}, // too long to be an enumerator
		{"3.", "", "", false, false},               // marker with no content
	}
	for _, tc := range cases {
		marker, rest, ordered, ok := listMarker(tc.in)
		if ok != tc.ok || marker != tc.marker || rest != tc.rest || ordered != tc.ordered {
			t.Errorf("listMarker(%q) = (%q, %q, %v, %v), want (%q, %q, %v, %v)",
				tc.in, marker, rest, ordered, ok, tc.marker, tc.rest, tc.ordered, tc.ok)
		}
	}
}
