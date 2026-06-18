package parserails

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

// Text concatenates all words across all pages, space-separated.
func (d *Document) Text() string {
	var b []byte
	for i := range d.Pages {
		for j := range d.Pages[i].Words {
			if len(b) > 0 {
				b = append(b, ' ')
			}
			b = append(b, d.Pages[i].Words[j].Text...)
		}
	}
	return string(b)
}
