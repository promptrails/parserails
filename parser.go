package parserails

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pdfium "github.com/klippa-app/go-pdfium"
	pdfium_errors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
)

// Granularity controls how text is segmented into Words.
type Granularity int

const (
	// GranularityWord groups individual characters into words split on
	// whitespace; each Word's box is the union of its characters' boxes. This is
	// the most precise mode and the default.
	GranularityWord Granularity = iota

	// GranularityLine returns PDFium's text rectangles (typically line or run
	// fragments) directly. It crosses the Go↔WASM boundary far fewer times, so
	// it is dramatically faster and lighter — at the cost of per-word precision.
	// Use it when line-level boxes are enough (e.g. RAG chunking, search).
	GranularityLine
)

// Parser extracts spatial text from documents using a pooled PDFium runtime.
//
// It is safe for concurrent use: each Parse call borrows an instance from the
// pool and returns it when done. Create one Parser per process and reuse it.
type Parser struct {
	pool        pdfium.Pool
	ocr         OCR
	granularity Granularity
	fontInfo    bool
	sofficeBin  string
	password    string
	maxPages    int
	imageOCR    bool
	images      bool
}

// Option configures a Parser.
type Option func(*config)

type config struct {
	minIdle, maxIdle, maxTotal int
	ocr                        OCR
	granularity                Granularity
	fontInfo                   bool
	sofficeBin                 string
	password                   string
	maxPages                   int
	imageOCR                   bool
	images                     bool
}

// WithOCR sets the OCR backend used as a fallback for pages with no extractable
// text. By default no OCR is performed.
func WithOCR(o OCR) Option { return func(c *config) { c.ocr = o } }

// WithGranularity selects how text is segmented into Words (default
// GranularityWord).
func WithGranularity(g Granularity) Option { return func(c *config) { c.granularity = g } }

// WithFontInfo collects per-character font size. It is off by default because it
// roughly doubles extraction cost. Only meaningful with GranularityWord.
func WithFontInfo() Option { return func(c *config) { c.fontInfo = true } }

// WithPoolSize tunes the underlying PDFium worker pool.
func WithPoolSize(minIdle, maxIdle, maxTotal int) Option {
	return func(c *config) { c.minIdle, c.maxIdle, c.maxTotal = minIdle, maxIdle, maxTotal }
}

// WithLibreOffice sets an explicit LibreOffice binary path used to convert
// office documents (DOCX/PPTX/XLSX/...) to PDF. By default ParseRails looks for
// the PARSERAILS_SOFFICE env var, then "soffice"/"libreoffice" on PATH.
func WithLibreOffice(path string) Option { return func(c *config) { c.sofficeBin = path } }

// WithImageOCR also recognizes the text inside raster figures on pages that do
// have a text layer, merging it with the native words. Without it, OCR only
// runs on pages with no extractable text at all.
//
// It costs one page render plus one OCR call per substantial figure, so it is
// off by default; it needs an OCR backend (WithOCR) to do anything.
func WithImageOCR() Option { return func(c *config) { c.imageOCR = true } }

// WithImages records the raster figures on each page (Page.Images), so
// Markdown output can place them and callers can crop them. It costs one call
// per page object, so it is off by default; WithImageOCR implies it.
func WithImages() Option { return func(c *config) { c.images = true } }

// WithPassword sets the default password used to open encrypted documents. It
// can be overridden per read with ReadOptions.Password.
func WithPassword(password string) Option { return func(c *config) { c.password = password } }

// WithMaxPages caps how many pages any single read parses. Zero (the default)
// means no cap; it can be overridden per read with ReadOptions.MaxPages.
func WithMaxPages(n int) Option { return func(c *config) { c.maxPages = n } }

// New initializes a Parser backed by a pure-Go PDFium WebAssembly runtime.
// No cgo and no system libraries are required. Call Close when finished.
func New(opts ...Option) (*Parser, error) {
	cfg := config{minIdle: 1, maxIdle: 2, maxTotal: 4, ocr: noOCR{}}
	for _, opt := range opts {
		opt(&cfg)
	}

	pool, err := newPool(poolConfig{minIdle: cfg.minIdle, maxIdle: cfg.maxIdle, maxTotal: cfg.maxTotal})
	if err != nil {
		return nil, fmt.Errorf("parserails: init pdfium pool: %w", err)
	}
	return &Parser{
		pool:        pool,
		ocr:         cfg.ocr,
		granularity: cfg.granularity,
		fontInfo:    cfg.fontInfo,
		sofficeBin:  cfg.sofficeBin,
		password:    cfg.password,
		maxPages:    cfg.maxPages,
		imageOCR:    cfg.imageOCR,
		images:      cfg.images || cfg.imageOCR,
	}, nil
}

// Close releases the PDFium runtime and all pooled workers.
func (p *Parser) Close() error { return p.pool.Close() }

// Parse extracts every page's words with bounding boxes from the given PDF
// data, using the parser's defaults. Use ParseData for input that may be in
// another format, or to select pages and pass a password per call.
func (p *Parser) Parse(ctx context.Context, data []byte) (*Document, error) {
	if f := Sniff(data); f != FormatPDF && f != FormatUnknown {
		return nil, fmt.Errorf("parserails: Parse wants PDF data, got %s; use ParseData", f)
	}
	return p.parsePDF(ctx, data, ReadOptions{})
}

// parsePDF is the PDF parsing core: every entry point funnels here once the
// input is known to be a PDF.
func (p *Parser) parsePDF(ctx context.Context, data []byte, opt ReadOptions) (*Document, error) {
	opt = p.withDefaults(opt)

	inst, err := p.pool.GetInstance(30 * time.Second)
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

	out := &Document{Pages: make([]Page, 0, len(pages))}
	for _, index := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := p.parsePage(ctx, inst, doc.Document, index)
		if err != nil {
			return nil, err
		}
		out.Pages = append(out.Pages, page)
	}
	return out, nil
}

// openDocument opens a PDF, translating PDFium's password errors into an error
// that says what is actually wrong.
func openDocument(inst pdfium.Pdfium, data []byte, password string) (*responses.OpenDocument, error) {
	req := &requests.OpenDocument{File: &data}
	if password != "" {
		req.Password = &password
	}
	doc, err := inst.OpenDocument(req)
	if err != nil {
		if errors.Is(err, pdfium_errors.ErrPassword) || strings.Contains(err.Error(), "invalid password") {
			if password == "" {
				return nil, fmt.Errorf("parserails: document is encrypted: %w", err)
			}
			return nil, fmt.Errorf("parserails: wrong password: %w", err)
		}
		return nil, fmt.Errorf("parserails: open document: %w", err)
	}
	return doc, nil
}

func (p *Parser) parsePage(ctx context.Context, inst pdfium.Pdfium, docRef references.FPDF_DOCUMENT, index int) (Page, error) {
	pageReq := requests.Page{ByIndex: &requests.PageByIndex{Document: docRef, Index: index}}

	size, err := inst.GetPageSize(&requests.GetPageSize{Page: pageReq})
	if err != nil {
		return Page{}, fmt.Errorf("parserails: page %d size: %w", index, err)
	}
	page := Page{Index: index, Width: size.Width, Height: size.Height}

	mode := requests.GetPageTextStructuredModeChars
	if p.granularity == GranularityLine {
		mode = requests.GetPageTextStructuredModeRects
	}
	text, err := inst.GetPageTextStructured(&requests.GetPageTextStructured{
		Page:                   pageReq,
		Mode:                   mode,
		CollectFontInformation: p.fontInfo && p.granularity == GranularityWord,
	})
	if err != nil {
		return Page{}, fmt.Errorf("parserails: page %d text: %w", index, err)
	}

	if p.granularity == GranularityLine {
		page.Words = wordsFromRects(text.Rects, index)
	} else {
		page.Words = wordsFromChars(text.Chars, index)
	}

	dims := pageSize{Width: size.Width, Height: size.Height}
	if p.images {
		images, err := pageImages(inst, pageReq)
		if err != nil {
			return Page{}, err
		}
		page.Images = images
	}
	switch {
	case len(page.Words) == 0:
		// Scanned/image-only page: no extractable text. Fall back to OCR.
		ocrWords, err := p.ocrPage(ctx, inst, pageReq, dims)
		if err != nil {
			return Page{}, err
		}
		page.Words = ocrWords
	case p.imageOCR && p.hasOCR():
		// Text page with figures: read what the pictures say too.
		words, err := p.ocrFigures(ctx, inst, pageReq, dims, page.Words, index, page.Images)
		if err != nil {
			return Page{}, err
		}
		page.Words = words
	}
	return page, nil
}

// hasOCR reports whether a real OCR backend is configured.
func (p *Parser) hasOCR() bool {
	_, none := p.ocr.(noOCR)
	return !none
}

// wordsFromChars groups characters into words split on whitespace; each word's
// box is the union of its characters' boxes.
func wordsFromChars(chars []*responses.GetPageTextStructuredChar, page int) []Word {
	words := make([]Word, 0, len(chars)/4+1)
	var (
		buf  strings.Builder
		cur  Word
		open bool
	)
	flush := func() {
		if open && buf.Len() > 0 {
			cur.Text = buf.String()
			words = append(words, cur)
		}
		buf.Reset()
		open = false
	}
	for _, c := range chars {
		if strings.TrimSpace(c.Text) == "" {
			flush()
			continue
		}
		pos := c.PointPosition
		if !open {
			cur = Word{Page: page, X0: pos.Left, Y0: pos.Bottom, X1: pos.Right, Y1: pos.Top}
			if c.FontInformation != nil {
				cur.FontSize = c.FontInformation.Size
			}
			open = true
		} else {
			cur.X0 = min(cur.X0, pos.Left)
			cur.Y0 = min(cur.Y0, pos.Bottom)
			cur.X1 = max(cur.X1, pos.Right)
			cur.Y1 = max(cur.Y1, pos.Top)
		}
		buf.WriteString(c.Text)
	}
	flush()
	return words
}

// wordsFromRects maps PDFium text rectangles directly to Words.
func wordsFromRects(rects []*responses.GetPageTextStructuredRect, page int) []Word {
	out := make([]Word, 0, len(rects))
	for _, r := range rects {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		p := r.PointPosition
		out = append(out, Word{
			Text: r.Text, Page: page,
			X0: p.Left, Y0: p.Bottom, X1: p.Right, Y1: p.Top,
		})
	}
	return out
}
