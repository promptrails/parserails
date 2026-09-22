package parserails

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// OfficeDocument is an office document read natively — straight out of the
// Office Open XML package, with no LibreOffice and no PDF in between.
//
// There is no page geometry here: blocks carry structure and text but no
// boxes, because nothing has been laid out. What it gives up in coordinates it
// gains in fidelity and cost — a DOCX says which paragraph is a heading and
// where a table's cells are, so none of that has to be inferred from
// positions, and a spreadsheet keeps its real rows instead of a rendering of
// them.
type OfficeDocument struct {
	Format Format
	Blocks []Block
}

// Text renders the document as plain text.
func (o *OfficeDocument) Text() string {
	var parts []string
	for _, b := range o.Blocks {
		switch b.Kind {
		case BlockTable:
			for _, row := range append([][]Cell{b.Header}, b.Rows...) {
				if line := joinCells(row); line != "" {
					parts = append(parts, line)
				}
			}
		default:
			if text := strings.TrimSpace(b.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// Markdown renders the document as Markdown.
func (o *OfficeDocument) Markdown() string { return renderMarkdown(o.Blocks) }

// joinCells renders a row as tab-separated text, keeping empty cells so the
// columns still line up — dropping them would shift every value left of a gap
// under the wrong header.
func joinCells(row []Cell) string {
	parts := make([]string, 0, len(row))
	last := -1
	for i, c := range row {
		text := strings.TrimSpace(c.Text)
		parts = append(parts, text)
		if text != "" {
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	return strings.Join(parts[:last+1], "\t")
}

// ReadOfficeDocument reads an OOXML package (DOCX, XLSX, PPTX) directly.
//
// Use it when LibreOffice is not available or not wanted — a scratch
// container, a lambda, a build with no system dependencies — or when the
// document's own structure is worth more than its rendered layout. Use
// ParseFile when you need coordinates.
func ReadOfficeDocument(data []byte, format Format) (*OfficeDocument, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("parserails: read ooxml package: %w", err)
	}
	var blocks []Block
	switch format {
	case FormatDOCX:
		blocks, err = docxBlocks(zr)
	case FormatXLSX:
		blocks, err = xlsxBlocks(zr)
	case FormatPPTX:
		blocks, err = pptxBlocks(zr)
	default:
		return nil, fmt.Errorf("parserails: %s is not an Office Open XML format", format)
	}
	if err != nil {
		return nil, err
	}
	return &OfficeDocument{Format: format, Blocks: blocks}, nil
}

// zipEntry reads one file out of a package.
func zipEntry(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(io.LimitReader(rc, maxChildBytes))
	}
	return nil, fmt.Errorf("parserails: %s missing from package", name)
}

// docxBlocks reads word/document.xml. Word marks its own structure —
// paragraph styles, numbering, table cells — so the reader follows the
// document rather than guessing from geometry.
func docxBlocks(zr *zip.Reader) ([]Block, error) {
	data, err := zipEntry(zr, "word/document.xml")
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(data))

	var (
		blocks []Block
		inBody bool
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parserails: read word/document.xml: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "body":
			inBody = true
		case "p":
			if !inBody {
				continue
			}
			if block, ok := docxParagraph(dec, start); ok {
				blocks = append(blocks, block)
			}
		case "tbl":
			if !inBody {
				continue
			}
			block, err := docxTable(dec, start)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

// docxParagraph reads one <w:p>, classifying it from its style and numbering.
func docxParagraph(dec *xml.Decoder, start xml.StartElement) (Block, bool) {
	var (
		text     strings.Builder
		style    string
		numbered bool
		depth    = 1
		skipAt   int
	)
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if skipAt == 0 && docxSkippable(t.Name.Local) {
				skipAt = depth
			}
			switch t.Name.Local {
			case "pStyle":
				style = attr(t, "val")
			case "numPr":
				numbered = true
			case "tab":
				text.WriteByte('\t')
			case "br":
				text.WriteByte('\n')
			}
		case xml.EndElement:
			if skipAt != 0 && depth == skipAt {
				skipAt = 0
			}
			depth--
			if depth == 0 && t.Name.Local == start.Name.Local {
				break
			}
		case xml.CharData:
			if skipAt == 0 {
				text.Write(t)
			}
		}
	}

	content := strings.TrimSpace(text.String())
	if content == "" {
		return Block{}, false
	}
	if level, ok := headingStyleLevel(style); ok {
		return Block{Kind: BlockHeading, Text: content, Level: level}, true
	}
	if numbered || strings.HasPrefix(strings.ToLower(style), "list") {
		marker, rest, ordered, isItem := listMarker(content)
		if !isItem {
			marker, rest, ordered = "-", content, false
		}
		return Block{Kind: BlockListItem, Text: rest, Marker: marker, Ordered: ordered}, true
	}
	return Block{Kind: BlockParagraph, Text: content}, true
}

// docxSkippable reports whether an element's character data is something other
// than the document's text: the original of a tracked deletion, or the source
// of a field code like " HYPERLINK … ". Both sit in ordinary runs and would
// otherwise be read as prose.
func docxSkippable(name string) bool {
	return name == "delText" || name == "instrText"
}

// headingStyleLevel maps Word's built-in styles onto heading depths.
func headingStyleLevel(style string) (int, bool) {
	normalized := strings.ToLower(strings.ReplaceAll(style, " ", ""))
	if normalized == "title" {
		return 1, true
	}
	rest, found := strings.CutPrefix(normalized, "heading")
	if !found {
		return 0, false
	}
	level, err := strconv.Atoi(rest)
	if err != nil || level < 1 {
		return 0, false
	}
	// Word's Heading 1 sits under the Title, so everything shifts one down,
	// capped at Markdown's six levels.
	return min(level+1, 6), true
}

// docxTable reads one <w:tbl> into a table block with its real cells.
func docxTable(dec *xml.Decoder, start xml.StartElement) (Block, error) {
	var (
		rows   [][]Cell
		row    []Cell
		cell   strings.Builder
		depth  = 1
		skipAt int
		inCel  bool
	)
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return Block{}, fmt.Errorf("parserails: read table: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if skipAt == 0 && docxSkippable(t.Name.Local) {
				skipAt = depth
			}
			switch t.Name.Local {
			case "tbl":
				// A table inside a cell: consumed whole, so its rows cannot
				// reset the row being built around it, and flattened into
				// the cell's text so nothing is lost.
				nested, err := docxTable(dec, t)
				if err != nil {
					return Block{}, err
				}
				depth--
				if inCel && skipAt == 0 {
					if text := flattenTable(nested); text != "" {
						cell.WriteString(text)
						cell.WriteByte('\n') // keep it off the next paragraph
					}
				}
			case "tr":
				row = nil
			case "tc":
				inCel = true
				cell.Reset()
			case "tab":
				if inCel && skipAt == 0 {
					cell.WriteByte('\t')
				}
			case "br":
				if inCel && skipAt == 0 {
					cell.WriteByte('\n')
				}
			}
		case xml.EndElement:
			if skipAt != 0 && depth == skipAt {
				skipAt = 0
			}
			depth--
			switch t.Name.Local {
			case "p":
				// A cell holds paragraphs, not one run of text: without a
				// boundary here "First paragraph" and "Second paragraph"
				// concatenate into one mangled word.
				if inCel {
					cell.WriteByte('\n')
				}
			case "tc":
				inCel = false
				row = append(row, Cell{Text: strings.TrimSpace(cell.String())})
			case "tr":
				if len(row) > 0 {
					rows = append(rows, row)
				}
			}
			if depth == 0 && t.Name.Local == start.Name.Local {
				break
			}
		case xml.CharData:
			if inCel && skipAt == 0 {
				cell.Write(t)
			}
		}
	}

	block := Block{Kind: BlockTable, Rows: rows}
	if len(rows) > 1 && complete(rows[0]) {
		block.Header, block.Rows = rows[0], rows[1:]
	}
	return block, nil
}

// flattenTable renders a nested table as plain text for the cell that holds
// it: rows on their own lines, cells separated by tabs.
func flattenTable(block Block) string {
	var lines []string
	for _, row := range append([][]Cell{block.Header}, block.Rows...) {
		if line := joinCells(row); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// xlsxBlocks reads every worksheet as a table, with the sheet name as a
// heading above it.
func xlsxBlocks(zr *zip.Reader) ([]Block, error) {
	strs := sharedStrings(zr)
	var blocks []Block
	for _, sheet := range worksheets(zr) {
		data, err := zipEntry(zr, sheet.path)
		if err != nil {
			continue
		}
		rows, err := sheetRows(data, strs)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}
		blocks = append(blocks, Block{Kind: BlockHeading, Text: sheet.name, Level: 2})
		block := Block{Kind: BlockTable, Rows: rows}
		// A sheet's first row is its header even when a column or two is
		// blank; spreadsheets are written that way.
		if len(rows) > 1 && mostlyFilled(rows[0]) {
			block.Header, block.Rows = rows[0], rows[1:]
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

type worksheet struct{ name, path string }

// worksheets lists the sheets in workbook order, resolving each one's part
// through the workbook relationships.
func worksheets(zr *zip.Reader) []worksheet {
	rels := relationships(zr, "xl/_rels/workbook.xml.rels")
	data, err := zipEntry(zr, "xl/workbook.xml")
	if err != nil {
		return nil
	}
	var out []worksheet
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}
		name := attr(start, "name")
		target := rels[relID(start)]
		if target == "" {
			continue
		}
		out = append(out, worksheet{name: name, path: normalizePart("xl", target)})
	}
	return out
}

func relationships(zr *zip.Reader, path string) map[string]string {
	out := map[string]string{}
	data, err := zipEntry(zr, path)
	if err != nil {
		return out
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "Relationship" {
			out[attr(start, "Id")] = attr(start, "Target")
		}
	}
	return out
}

func normalizePart(base, target string) string {
	target = strings.TrimPrefix(target, "/")
	if strings.HasPrefix(target, base+"/") {
		return target
	}
	return base + "/" + target
}

func sharedStrings(zr *zip.Reader) []string {
	data, err := zipEntry(zr, "xl/sharedStrings.xml")
	if err != nil {
		return nil
	}
	var (
		out     []string
		current strings.Builder
		inItem  bool
	)
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				inItem = true
				current.Reset()
			}
		case xml.EndElement:
			if t.Name.Local == "si" && inItem {
				inItem = false
				out = append(out, current.String())
			}
		case xml.CharData:
			if inItem {
				current.Write(t)
			}
		}
	}
	return out
}

// sheetRows reads a worksheet's cells, honouring the column each one declares
// so sparse rows still line up.
//
// Two details of the format decide the shape of this loop: a cell may carry
// several rich-text runs, each in its own <t>, which belong to one value; and
// the r="B3" reference is optional — streaming writers omit it and mean "the
// next column".
func sheetRows(data []byte, strs []string) ([][]Cell, error) {
	var (
		rows     []map[int]string
		row      map[int]string
		width    int
		column   int
		nextCol  int
		cellTyp  string
		cellText strings.Builder
		value    strings.Builder
		inValue  bool
	)
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parserails: read worksheet: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = map[int]string{}
				nextCol = 0
			case "c":
				cellTyp = attr(t, "t")
				cellText.Reset()
				column = columnIndex(attr(t, "r"))
				if column < 0 {
					column = nextCol // absent or unusable reference
				}
				nextCol = column + 1
			case "v", "t":
				inValue = true
				value.Reset()
			}
		case xml.CharData:
			if inValue {
				value.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inValue = false
				text := value.String()
				if cellTyp == "s" {
					if i, err := strconv.Atoi(text); err == nil && i >= 0 && i < len(strs) {
						text = strs[i]
					}
				}
				cellText.WriteString(text)
			case "c":
				if row == nil || cellText.Len() == 0 {
					continue
				}
				row[column] = cellText.String()
				width = max(width, column+1)
				cellText.Reset()
			case "row":
				if len(row) > 0 {
					rows = append(rows, row)
				}
				row = nil
			}
		}
	}
	return densify(rows, width), nil
}

// Limits on expanding a worksheet into a grid. Columns run to XFD, so a
// handful of values parked at the far right would otherwise become 16384
// cells per row — fifteen megabytes for twenty numbers, gigabytes for a sheet
// with a few thousand rows.
const (
	maxSheetCells    = 1 << 20 // never expand past this many cells
	denseGridFloor   = 4096    // below this, expand whatever the shape
	denseGridDivisor = 8       // expand while at least 1/8 of the grid is filled
)

// densify turns sparse rows into a rectangular grid, keeping blank columns so
// values stay under their headers. A sheet too sparse to expand that way is
// compacted onto the columns that actually carry something: the gaps are lost,
// which is worth it against the alternative of not reading the sheet at all.
func densify(rows []map[int]string, width int) [][]Cell {
	if len(rows) == 0 || width == 0 {
		return nil
	}

	total := int64(width) * int64(len(rows))
	var filled int64
	for _, row := range rows {
		filled += int64(len(row))
	}

	columns := make([]int, 0, width)
	// Expand to the real grid while it is reasonably full; compact when it
	// would be mostly empty, or simply too large.
	if total > maxSheetCells || (total > denseGridFloor && filled*denseGridDivisor < total) {
		used := map[int]bool{}
		for _, row := range rows {
			for col := range row {
				used[col] = true
			}
		}
		for col := range used {
			columns = append(columns, col)
		}
		sort.Ints(columns)
	} else {
		for col := 0; col < width; col++ {
			columns = append(columns, col)
		}
	}

	// Compaction is not a bound: a sheet with one value per row, each in a
	// different column, has as many columns as rows. When even the compacted
	// grid is too large, the rows are left ragged — every value kept, in
	// column order, with no alignment between rows. Alignment is what cannot
	// be afforded here; the data is not.
	if int64(len(rows))*int64(len(columns)) > maxSheetCells {
		return raggedRows(rows)
	}

	at := make(map[int]int, len(columns))
	for i, col := range columns {
		at[col] = i
	}
	out := make([][]Cell, 0, len(rows))
	for _, row := range rows {
		cells := make([]Cell, len(columns))
		for col, text := range row {
			if i, ok := at[col]; ok {
				cells[i] = Cell{Text: text}
			}
		}
		out = append(out, cells)
	}
	return out
}

// raggedRows emits each row's own values in column order, without expanding
// them onto a shared grid.
func raggedRows(rows []map[int]string) [][]Cell {
	out := make([][]Cell, 0, len(rows))
	for _, row := range rows {
		columns := make([]int, 0, len(row))
		for col := range row {
			columns = append(columns, col)
		}
		sort.Ints(columns)

		cells := make([]Cell, 0, len(columns))
		for _, col := range columns {
			cells = append(cells, Cell{Text: row[col]})
		}
		out = append(out, cells)
	}
	return out
}

// maxSpreadsheetColumn is Excel's last column, XFD. A reference past it is not
// a reference: read as a number, a long run of letters overflows int and then
// indexes a slice with a negative column.
const maxSpreadsheetColumn = 16384

// columnIndex turns a cell reference like "BC12" into a 0-based column, or -1
// when the reference is absent or not one.
func columnIndex(ref string) int {
	col, letters := 0, 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		letters++
		if letters > 3 {
			return -1
		}
		col = col*26 + int(r-'A') + 1
	}
	if col == 0 || col > maxSpreadsheetColumn {
		return -1
	}
	return col - 1
}

// pptxBlocks reads each slide in presentation order: its first text is the
// title, the rest is body text.
func pptxBlocks(zr *zip.Reader) ([]Block, error) {
	slides := slideOrder(zr)
	if len(slides) == 0 {
		slides = slidesByName(zr) // no presentation part: fall back to names
	}

	var blocks []Block
	for _, name := range slides {
		data, err := zipEntry(zr, name)
		if err != nil {
			continue
		}
		paragraphs := slideParagraphs(data)
		for i, text := range paragraphs {
			if i == 0 {
				blocks = append(blocks, Block{Kind: BlockHeading, Text: text, Level: 2})
				continue
			}
			blocks = append(blocks, Block{Kind: BlockParagraph, Text: text})
		}
	}
	return blocks, nil
}

// slideOrder resolves the presentation's own slide order.
//
// Part names do not carry it: moving a slide in PowerPoint rewrites the
// <p:sldIdLst> in presentation.xml and leaves slide3.xml called slide3.xml, so
// sorting by file name reads a reordered deck in the wrong order.
func slideOrder(zr *zip.Reader) []string {
	rels := relationships(zr, "ppt/_rels/presentation.xml.rels")
	data, err := zipEntry(zr, "ppt/presentation.xml")
	if err != nil {
		return nil
	}
	var out []string
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "sldId" {
			continue
		}
		target := rels[relID(start)]
		if target == "" {
			continue
		}
		out = append(out, normalizePart("ppt", target))
	}
	return out
}

// slidesByName lists the slide parts in numeric file-name order.
func slidesByName(zr *zip.Reader) []string {
	var slides []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slides = append(slides, f.Name)
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slideNumber(slides[i]) < slideNumber(slides[j]) })
	return slides
}

func slideNumber(name string) int {
	digits := strings.TrimSuffix(strings.TrimPrefix(name, "ppt/slides/slide"), ".xml")
	n, _ := strconv.Atoi(digits)
	return n
}

// slideParagraphs collects each <a:p> on a slide as one line of text.
func slideParagraphs(data []byte) []string {
	var (
		out     []string
		current strings.Builder
		inPara  bool
	)
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "p" {
				inPara = true
				current.Reset()
			}
		case xml.EndElement:
			if t.Name.Local == "p" && inPara {
				inPara = false
				if text := strings.TrimSpace(current.String()); text != "" {
					out = append(out, text)
				}
			}
		case xml.CharData:
			if inPara {
				current.Write(t)
			}
		}
	}
	return out
}

// mostlyFilled reports whether at least half a row's cells carry text.
func mostlyFilled(row []Cell) bool {
	filled := 0
	for _, c := range row {
		if strings.TrimSpace(c.Text) != "" {
			filled++
		}
	}
	return len(row) > 0 && filled*2 >= len(row)
}

// relID returns an element's r:id relationship reference. It cannot just look
// for "id": a <p:sldId> carries both a plain id="256" and the r:id="rId2" that
// actually names the part, and both have the local name "id".
func relID(e xml.StartElement) string {
	for _, a := range e.Attr {
		if a.Name.Local == "id" && a.Name.Space != "" {
			return a.Value
		}
	}
	for _, a := range e.Attr {
		if a.Name.Local == "id" && strings.HasPrefix(a.Value, "rId") {
			return a.Value
		}
	}
	return ""
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
