package parserails

import "strings"

// Word is a single extracted token with its spatial bounding box.
//
// Coordinates are in PDF user space (origin at the bottom-left of the page),
// matching what the PDFium text engine reports. X0,Y0 is the lower-left corner
// and X1,Y1 the upper-right corner of the box.
type Word struct {
	Text string  `json:"text"`
	Page int     `json:"page"`
	X0   float64 `json:"x0"`
	Y0   float64 `json:"y0"`
	X1   float64 `json:"x1"`
	Y1   float64 `json:"y1"`
	// FontSize is the font size in points. It is 0 unless the parser was created
	// with WithFontInfo (and GranularityWord).
	FontSize float64 `json:"font_size,omitempty"`
}

// Page holds the words extracted from a single page plus its dimensions.
type Page struct {
	Index  int     `json:"index"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Words  []Word  `json:"words"`
}

// Document is the result of parsing one input file.
type Document struct {
	Pages []Page `json:"pages"`
}

// Words flattens every page's words into a single slice in reading order.
func (d *Document) Words() []Word {
	var out []Word
	for i := range d.Pages {
		out = append(out, d.Pages[i].Words...)
	}
	return out
}

// Text returns the document's text with its line structure reconstructed:
// words are grouped into lines by geometry, lines are joined with newlines, and
// pages are separated by a form feed ("\f").
//
// Reading order is top-to-bottom, then left-to-right. Multi-column pages are
// read line-by-line across the whole page width; use Blocks or Markdown when
// column structure matters.
func (d *Document) Text() string {
	var b strings.Builder
	for i := range d.Pages {
		if i > 0 {
			b.WriteByte('\f')
		}
		b.WriteString(d.Pages[i].Text())
	}
	return b.String()
}
