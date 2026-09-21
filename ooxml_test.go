package parserails

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestReadOfficeDocumentDOCX(t *testing.T) {
	docx := zipArchive(map[string][]byte{
		"word/document.xml": []byte(`<?xml version="1.0"?>
<w:document xmlns:w="x"><w:body>
  <w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>Quarterly Report</w:t></w:r></w:p>
  <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Summary</w:t></w:r></w:p>
  <w:p><w:r><w:t>Revenue grew </w:t></w:r><w:r><w:t>across every region.</w:t></w:r></w:p>
  <w:p><w:pPr><w:numPr><w:ilvl w:val="0"/></w:numPr></w:pPr><w:r><w:t>Renewals up</w:t></w:r></w:p>
  <w:tbl>
    <w:tr><w:tc><w:p><w:r><w:t>Region</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Revenue</w:t></w:r></w:p></w:tc></w:tr>
    <w:tr><w:tc><w:p><w:r><w:t>EMEA</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>1.2M</w:t></w:r></w:p></w:tc></w:tr>
  </w:tbl>
</w:body></w:document>`),
	})

	doc, err := ReadOfficeDocument(docx, FormatDOCX)
	if err != nil {
		t.Fatalf("ReadOfficeDocument: %v", err)
	}
	var kinds []string
	for _, b := range doc.Blocks {
		kinds = append(kinds, string(b.Kind))
	}
	want := "heading heading paragraph list_item table"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("kinds = %q, want %q", got, want)
	}
	if doc.Blocks[0].Level != 1 || doc.Blocks[1].Level != 2 {
		t.Errorf("levels = %d, %d; want the Title above Heading 1",
			doc.Blocks[0].Level, doc.Blocks[1].Level)
	}
	// Runs inside a paragraph are one paragraph, not three.
	if got := doc.Blocks[2].Text; got != "Revenue grew across every region." {
		t.Errorf("paragraph = %q", got)
	}
	table := doc.Blocks[4]
	if len(table.Header) != 2 || table.Header[0].Text != "Region" || table.Rows[0][1].Text != "1.2M" {
		t.Errorf("table = %+v", table)
	}

	md := doc.Markdown()
	for _, want := range []string{"# Quarterly Report", "## Summary", "- Renewals up", "| EMEA | 1.2M |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestDocxCellsKeepParagraphBoundaries(t *testing.T) {
	docx := zipArchive(map[string][]byte{
		"word/document.xml": []byte(`<w:document xmlns:w="x"><w:body><w:tbl><w:tr>` +
			`<w:tc><w:p><w:r><w:t>First paragraph</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>Second paragraph</w:t></w:r></w:p></w:tc>` +
			`<w:tc><w:p><w:r><w:t>Other</w:t></w:r></w:p></w:tc>` +
			`</w:tr></w:tbl></w:body></w:document>`),
	})
	doc, err := ReadOfficeDocument(docx, FormatDOCX)
	if err != nil {
		t.Fatalf("ReadOfficeDocument: %v", err)
	}
	cell := doc.Blocks[0].Rows[0][0].Text
	if cell != "First paragraph\nSecond paragraph" {
		t.Fatalf("cell = %q, want the paragraphs separated", cell)
	}
	// A pipe table cannot hold a newline; the break survives as <br>.
	if md := doc.Markdown(); !strings.Contains(md, "First paragraph<br>Second paragraph") {
		t.Errorf("markdown = %q", md)
	}
}

func TestDocxSkipsTrackedDeletionsAndFieldCodes(t *testing.T) {
	docx := zipArchive(map[string][]byte{
		"word/document.xml": []byte(`<w:document xmlns:w="x"><w:body><w:p>` +
			`<w:r><w:t>The rate is </w:t></w:r>` +
			`<w:del><w:r><w:delText>four</w:delText></w:r></w:del>` +
			`<w:ins><w:r><w:t>five</w:t></w:r></w:ins>` +
			`<w:r><w:instrText> HYPERLINK "http://example.com" </w:instrText></w:r>` +
			`<w:r><w:t> percent.</w:t></w:r>` +
			`</w:p></w:body></w:document>`),
	})
	doc, err := ReadOfficeDocument(docx, FormatDOCX)
	if err != nil {
		t.Fatalf("ReadOfficeDocument: %v", err)
	}
	// The accepted revision reads; the deleted original and the field code do not.
	if got := doc.Blocks[0].Text; got != "The rate is five percent." {
		t.Fatalf("paragraph = %q", got)
	}
}

func TestReadOfficeDocumentXLSX(t *testing.T) {
	xlsx := zipArchive(map[string][]byte{
		"xl/workbook.xml": []byte(`<workbook xmlns:r="x"><sheets>` +
			`<sheet name="Q1" sheetId="1" r:id="rId1"/></sheets></workbook>`),
		"xl/_rels/workbook.xml.rels": []byte(
			`<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`),
		"xl/sharedStrings.xml": []byte(`<sst><si><t>Region</t></si><si><t>EMEA</t></si></sst>`),
		"xl/worksheets/sheet1.xml": []byte(`<worksheet><sheetData>` +
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1"><v>2024</v></c></row>` +
			`<row r="2"><c r="A2" t="s"><v>1</v></c><c r="C2"><v>1200000</v></c></row>` +
			`</sheetData></worksheet>`),
	})

	doc, err := ReadOfficeDocument(xlsx, FormatXLSX)
	if err != nil {
		t.Fatalf("ReadOfficeDocument: %v", err)
	}
	if len(doc.Blocks) != 2 || doc.Blocks[0].Text != "Q1" {
		t.Fatalf("blocks = %+v, want a sheet heading and a table", doc.Blocks)
	}
	table := doc.Blocks[1]
	// The gap at column B is preserved: C is the third column, not the second.
	if len(table.Header) != 3 || table.Header[0].Text != "Region" || table.Header[2].Text != "2024" {
		t.Fatalf("header = %+v", table.Header)
	}
	if table.Rows[0][2].Text != "1200000" {
		t.Errorf("row = %+v", table.Rows[0])
	}
}

func TestReadOfficeDocumentPPTX(t *testing.T) {
	slide := func(title, body string) []byte {
		return []byte(fmt.Sprintf(`<p:sld xmlns:a="x"><p:cSld><p:spTree>`+
			`<a:p><a:r><a:t>%s</a:t></a:r></a:p><a:p><a:r><a:t>%s</a:t></a:r></a:p>`+
			`</p:spTree></p:cSld></p:sld>`, title, body))
	}
	pptx := zipArchive(map[string][]byte{
		"ppt/presentation.xml":   []byte("<p:presentation/>"),
		"ppt/slides/slide1.xml":  slide("First slide", "first body"),
		"ppt/slides/slide2.xml":  slide("Second slide", "second body"),
		"ppt/slides/slide10.xml": slide("Tenth slide", "tenth body"),
	})

	doc, err := ReadOfficeDocument(pptx, FormatPPTX)
	if err != nil {
		t.Fatalf("ReadOfficeDocument: %v", err)
	}
	text := doc.Text()
	// slide10 must come after slide2, not between slide1 and slide2.
	if i, j := strings.Index(text, "Second slide"), strings.Index(text, "Tenth slide"); i > j {
		t.Errorf("slides out of order:\n%s", text)
	}
	if doc.Blocks[0].Kind != BlockHeading || doc.Blocks[1].Kind != BlockParagraph {
		t.Errorf("blocks = %+v, want a title then body", doc.Blocks[:2])
	}
}

func TestSheetRowsHandlesRichTextAndMissingRefs(t *testing.T) {
	// An inline cell built from two runs, then three cells with no r=.
	rows, err := sheetRows([]byte(`<worksheet><sheetData>`+
		`<row r="1"><c r="A1" t="inlineStr"><is><r><t>Hello </t></r><r><t>world</t></r></is></c></row>`+
		`<row r="2"><c><v>1</v></c><c><v>2</v></c><c><v>3</v></c></row>`+
		`</sheetData></worksheet>`), nil)
	if err != nil {
		t.Fatalf("sheetRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if got := rows[0][0].Text; got != "Hello world" {
		t.Errorf("rich-text cell = %q, want the runs joined", got)
	}
	if len(rows[1]) != 3 || rows[1][0].Text != "1" || rows[1][2].Text != "3" {
		t.Errorf("cells without r = %+v, want three columns", rows[1])
	}
}

func TestSheetRowsSurvivesImpossibleReference(t *testing.T) {
	rows, err := sheetRows([]byte(`<worksheet><sheetData><row r="1">`+
		`<c r="ZZZZZZZZZZZZZZ1"><v>9</v></c></row></sheetData></worksheet>`), nil)
	if err != nil {
		t.Fatalf("sheetRows: %v", err)
	}
	if len(rows) != 1 || rows[0][0].Text != "9" {
		t.Fatalf("rows = %+v, want the value in the first column", rows)
	}
}

func TestOfficeTextKeepsColumnPositions(t *testing.T) {
	office := &OfficeDocument{Blocks: []Block{{
		Kind: BlockTable,
		Rows: [][]Cell{{{Text: "A"}, {}, {Text: "C"}, {}}},
	}}}
	if got := office.Text(); got != "A\t\tC" {
		t.Fatalf("text = %q, want A\\t\\tC (interior gap kept, trailing dropped)", got)
	}
}

func TestNativeOfficeTextNeedsNoLibreOffice(t *testing.T) {
	docx := zipArchive(map[string][]byte{
		"word/document.xml": []byte(`<w:document xmlns:w="x"><w:body>` +
			`<w:p><w:r><w:t>No conversion needed.</w:t></w:r></w:p></w:body></w:document>`),
	})
	p := newTestParser(t, WithNativeOffice(), WithLibreOffice("/nonexistent/soffice"))

	text, err := p.ExtractTextData(context.Background(), docx, ReadOptions{Name: "memo.docx"})
	if err != nil {
		t.Fatalf("ExtractTextData: %v", err)
	}
	if text != "No conversion needed." {
		t.Fatalf("text = %q", text)
	}

	node, err := p.Extract(context.Background(), zipArchive(map[string][]byte{"memo.docx": docx}),
		ExtractOptions{ReadOptions: ReadOptions{Name: "bundle.zip"}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got := node.AllText(); !strings.Contains(got, "No conversion needed.") {
		t.Fatalf("extracted text = %q", got)
	}
}

func TestColumnIndex(t *testing.T) {
	cases := map[string]int{
		"A1": 0, "B2": 1, "Z9": 25, "AA1": 26, "BC12": 54,
		"XFD1": maxSpreadsheetColumn - 1,
		// Absent or impossible references are rejected, not wrapped around:
		// a long run of letters used to overflow into a negative index.
		"": -1, "ZZZZZZZZZZZZZZ1": -1, "ZZZZ1": -1,
	}
	for ref, want := range cases {
		if got := columnIndex(ref); got != want {
			t.Errorf("columnIndex(%q) = %d, want %d", ref, got, want)
		}
	}
}
