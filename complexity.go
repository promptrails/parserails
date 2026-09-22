package parserails

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
)

// Reasons a page is flagged as needing OCR.
const (
	// ReasonScanned marks a page covered by one raster with no text behind it.
	ReasonScanned = "scanned"
	// ReasonNoText marks a page with almost no extractable text and no
	// full-page image — a blank page, a cover, a divider.
	ReasonNoText = "no-text"
	// ReasonSparseText marks real text that covers very little of the page,
	// typically a figure with a thin caption.
	ReasonSparseText = "sparse-text"
	// ReasonEmbeddedImages marks substantial raster figures next to the text.
	ReasonEmbeddedImages = "embedded-images"
	// ReasonGarbled marks text that decodes to nonsense, usually a broken font
	// encoding, where re-reading the pixels beats trusting the text layer.
	ReasonGarbled = "garbled"
)

// Thresholds for the verdict. They are deliberately blunt: this is a router,
// not a classifier, and every page it flags can still be parsed normally.
const (
	fullPageImageCoverage    = 0.75
	scannedTextCoverage      = 0.02
	sparseTextCoverage       = 0.03
	substantialImageCoverage = 0.15
	minMeaningfulTextLength  = 16
	garbledRatio             = 0.2
)

// PageComplexity is the per-page verdict of Inspect.
type PageComplexity struct {
	Page                 int      `json:"page"` // 0-based page index
	TextLength           int      `json:"text_length"`
	TextCoverage         float64  `json:"text_coverage"`
	ImageCount           int      `json:"image_count"`
	ImageCoverage        float64  `json:"image_coverage"`
	LargestImageCoverage float64  `json:"largest_image_coverage"`
	FullPageImage        bool     `json:"full_page_image"`
	Garbled              bool     `json:"garbled"`
	NeedsOCR             bool     `json:"needs_ocr"`
	Reasons              []string `json:"reasons,omitempty"`
}

// Complexity is what Inspect reports about a document.
type Complexity struct {
	Pages []PageComplexity `json:"pages"`
}

// NeedsOCR reports whether any inspected page needs OCR.
func (c *Complexity) NeedsOCR() bool {
	for i := range c.Pages {
		if c.Pages[i].NeedsOCR {
			return true
		}
	}
	return false
}

// OCRPages lists the 0-based indexes of the pages that need OCR.
func (c *Complexity) OCRPages() []int {
	var out []int
	for i := range c.Pages {
		if c.Pages[i].NeedsOCR {
			out = append(out, c.Pages[i].Page)
		}
	}
	return out
}

// Inspect reports, page by page, whether a document needs OCR — before you
// commit to parsing it.
//
// It is a text-layer pass: page text, the area that text covers, and the raster
// images drawn on the page. No page is rendered and no OCR runs, so it costs a
// fraction of a parse. Use it to route documents to a cheap or an expensive
// pipeline, to reject what you cannot handle, or to price a batch by counting
// the pages that will actually need OCR.
func (p *Parser) Inspect(ctx context.Context, data []byte, opt ReadOptions) (*Complexity, error) {
	format := Detect(opt.Name, data)
	switch {
	case format == FormatPDF:
	case format.IsOffice():
		pdf, err := p.convertDataToPDF(ctx, data, format)
		if err != nil {
			return nil, err
		}
		data = pdf
	case format.IsImage():
		return imageComplexity(), nil
	default:
		return nil, unparsableError(opt.Name, format)
	}
	return p.inspectPDF(ctx, data, opt)
}

// InspectFile is the file counterpart of Inspect.
func (p *Parser) InspectFile(ctx context.Context, path string) (*Complexity, error) {
	data, format, err := readAndDetect(path)
	if err != nil {
		return nil, err
	}
	switch {
	case format == FormatPDF:
	case format.IsOffice():
		pdf, err := p.convertToPDF(ctx, path)
		if err != nil {
			return nil, err
		}
		data = pdf
	case format.IsImage():
		return imageComplexity(), nil
	default:
		return nil, unparsableError(path, format)
	}
	return p.inspectPDF(ctx, data, ReadOptions{Name: path})
}

// imageComplexity is the verdict for a standalone raster: it is a scan by
// definition, and nothing but OCR will read it.
func imageComplexity() *Complexity {
	return &Complexity{Pages: []PageComplexity{{
		Page:                 0,
		ImageCount:           1,
		ImageCoverage:        1,
		LargestImageCoverage: 1,
		FullPageImage:        true,
		NeedsOCR:             true,
		Reasons:              []string{ReasonScanned},
	}}}
}

func (p *Parser) inspectPDF(ctx context.Context, data []byte, opt ReadOptions) (*Complexity, error) {
	return bounded(ctx, p, func(ctx context.Context) (*Complexity, error) {
		return p.inspectPDFUnbounded(ctx, data, opt)
	})
}

func (p *Parser) inspectPDFUnbounded(ctx context.Context, data []byte, opt ReadOptions) (*Complexity, error) {
	opt = p.withDefaults(opt)

	inst, err := p.pool.GetInstance(p.acquireTimeout())
	if err != nil {
		return nil, fmt.Errorf("parserails: acquire instance: %w", err)
	}
	defer func() { _ = inst.Close() }()

	doc, err := openDocument(inst, data, opt.Password)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	}()

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, fmt.Errorf("parserails: page count: %w", err)
	}
	pages, err := selectPages(count.PageCount, opt)
	if err != nil {
		return nil, err
	}

	out := &Complexity{Pages: make([]PageComplexity, 0, len(pages))}
	for _, index := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := inspectPage(inst, doc.Document, index)
		if err != nil {
			return nil, err
		}
		out.Pages = append(out.Pages, page)
	}
	return out, nil
}

func inspectPage(inst pdfium.Pdfium, docRef references.FPDF_DOCUMENT, index int) (PageComplexity, error) {
	pageReq := requests.Page{ByIndex: &requests.PageByIndex{Document: docRef, Index: index}}

	size, err := inst.GetPageSize(&requests.GetPageSize{Page: pageReq})
	if err != nil {
		return PageComplexity{}, fmt.Errorf("parserails: page %d size: %w", index, err)
	}
	pageArea := size.Width * size.Height

	text, err := inst.GetPageText(&requests.GetPageText{Page: pageReq})
	if err != nil {
		return PageComplexity{}, fmt.Errorf("parserails: page %d text: %w", index, err)
	}
	// Text rectangles are the cheap way to measure how much of the page the
	// text layer actually inks: one call, no per-character crossing.
	rects, err := inst.GetPageTextStructured(&requests.GetPageTextStructured{
		Page: pageReq, Mode: requests.GetPageTextStructuredModeRects,
	})
	if err != nil {
		return PageComplexity{}, fmt.Errorf("parserails: page %d text rects: %w", index, err)
	}

	out := PageComplexity{Page: index}
	out.TextLength = len([]rune(strings.TrimSpace(text.Text)))
	out.Garbled = isGarbled(text.Text)

	var textArea float64
	for _, r := range rects.Rects {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		pos := r.PointPosition
		textArea += (pos.Right - pos.Left) * (pos.Top - pos.Bottom)
	}

	images, err := pageImages(inst, pageReq)
	if err != nil {
		return PageComplexity{}, err
	}
	var imageArea, largest float64
	for _, img := range images {
		area := img.Area()
		imageArea += area
		largest = max(largest, area)
	}

	if pageArea > 0 {
		out.TextCoverage = clamp01(textArea / pageArea)
		out.ImageCoverage = clamp01(imageArea / pageArea)
		out.LargestImageCoverage = clamp01(largest / pageArea)
	}
	out.ImageCount = len(images)
	out.FullPageImage = out.LargestImageCoverage >= fullPageImageCoverage
	out.Reasons = reasonsFor(out)
	out.NeedsOCR = len(out.Reasons) > 0
	return out, nil
}

// reasonsFor applies the verdict rules to one page's measurements.
func reasonsFor(c PageComplexity) []string {
	var reasons []string
	switch {
	case c.FullPageImage && c.TextCoverage < scannedTextCoverage:
		reasons = append(reasons, ReasonScanned)
	case c.TextLength < minMeaningfulTextLength:
		reasons = append(reasons, ReasonNoText)
	case c.TextCoverage < sparseTextCoverage && c.ImageCount > 0:
		reasons = append(reasons, ReasonSparseText)
	}
	if c.ImageCoverage >= substantialImageCoverage && !contains(reasons, ReasonScanned) {
		reasons = append(reasons, ReasonEmbeddedImages)
	}
	if c.Garbled {
		reasons = append(reasons, ReasonGarbled)
	}
	return reasons
}

// isGarbled reports whether a page's text decodes to nonsense. Broken font
// encodings produce replacement characters, private-use glyphs or literal
// "(cid:NN)" runs instead of readable text, and re-reading the pixels beats
// indexing that.
func isGarbled(text string) bool {
	if strings.Contains(text, "(cid:") {
		return true
	}
	var total, bad int
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		switch {
		case r == '�',
			r >= '' && r <= '', // private use area
			unicode.IsControl(r):
			bad++
		}
	}
	return total >= minMeaningfulTextLength && float64(bad)/float64(total) >= garbledRatio
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}
