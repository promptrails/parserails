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

func joinCells(row []Cell) string {
	parts := make([]string, 0, len(row))
	for _, c := range row {
		if text := strings.TrimSpace(c.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\t")
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
	)
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
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
			depth--
			if depth == 0 && t.Name.Local == start.Name.Local {
				break
			}
		case xml.CharData:
			text.Write(t)
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
		rows  [][]Cell
		row   []Cell
		cell  strings.Builder
		depth = 1
		inCel bool
	)
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return Block{}, fmt.Errorf("parserails: read table: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "tr":
				row = nil
			case "tc":
				inCel = true
				cell.Reset()
			}
		case xml.EndElement:
			depth--
			switch t.Name.Local {
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
			if inCel {
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
		target := rels[attr(start, "id")]
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
func sheetRows(data []byte, strs []string) ([][]Cell, error) {
	var (
		rows    [][]Cell
		row     map[int]string
		width   int
		colOf   int
		cellTyp string
		value   strings.Builder
		inValue bool
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
			case "c":
				colOf = columnIndex(attr(t, "r"))
				cellTyp = attr(t, "t")
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
				if row == nil {
					continue
				}
				text := value.String()
				if cellTyp == "s" {
					if i, err := strconv.Atoi(text); err == nil && i >= 0 && i < len(strs) {
						text = strs[i]
					}
				}
				if text != "" {
					row[colOf] = text
					width = max(width, colOf+1)
				}
			case "row":
				if len(row) > 0 {
					rows = append(rows, nil) // filled once the width is known
					rows[len(rows)-1] = spreadRow(row, width)
				}
				row = nil
			}
		}
	}
	return padRows(rows, width), nil
}

func spreadRow(row map[int]string, width int) []Cell {
	cells := make([]Cell, width)
	for col, text := range row {
		if col < width {
			cells[col] = Cell{Text: text}
		}
	}
	return cells
}

func padRows(rows [][]Cell, width int) [][]Cell {
	for i, row := range rows {
		for len(row) < width {
			row = append(row, Cell{})
		}
		rows[i] = row
	}
	return rows
}

// columnIndex turns a cell reference like "BC12" into a 0-based column.
func columnIndex(ref string) int {
	col := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		col = col*26 + int(r-'A') + 1
	}
	if col == 0 {
		return 0
	}
	return col - 1
}

// pptxBlocks reads each slide in order: its first text is the title, the rest
// is body text.
func pptxBlocks(zr *zip.Reader) ([]Block, error) {
	var slides []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slides = append(slides, f.Name)
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slideNumber(slides[i]) < slideNumber(slides[j]) })

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

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
