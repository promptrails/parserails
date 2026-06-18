package parserails

import (
	"context"
	"fmt"
	"strings"
	"time"

	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
	"github.com/klippa-app/go-pdfium/webassembly"
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
}

// Option configures a Parser.
type Option func(*config)

type config struct {
	minIdle, maxIdle, maxTotal int
	ocr                        OCR
	granularity                Granularity
	fontInfo                   bool
	sofficeBin                 string
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

// New initializes a Parser backed by a pure-Go PDFium WebAssembly runtime.
// No cgo and no system libraries are required. Call Close when finished.
func New(opts ...Option) (*Parser, error) {
	cfg := config{minIdle: 1, maxIdle: 2, maxTotal: 4, ocr: noOCR{}}
	for _, opt := range opts {
		opt(&cfg)
	}

	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  cfg.minIdle,
		MaxIdle:  cfg.maxIdle,
		MaxTotal: cfg.maxTotal,
	})
	if err != nil {
		return nil, fmt.Errorf("parserails: init pdfium pool: %w", err)
	}
	return &Parser{
		pool:        pool,
		ocr:         cfg.ocr,
		granularity: cfg.granularity,
		fontInfo:    cfg.fontInfo,
		sofficeBin:  cfg.sofficeBin,
	}, nil
}

// Close releases the PDFium runtime and all pooled workers.
func (p *Parser) Close() error { return p.pool.Close() }

// Parse extracts every page's words with bounding boxes from the given PDF data.
func (p *Parser) Parse(ctx context.Context, data []byte) (*Document, error) {
	inst, err := p.pool.GetInstance(30 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("parserails: acquire instance: %w", err)
	}
	defer func() { _ = inst.Close() }()

	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return nil, fmt.Errorf("parserails: open document: %w", err)
	}
	defer func() {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	}()

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, fmt.Errorf("parserails: page count: %w", err)
	}

	out := &Document{Pages: make([]Page, 0, count.PageCount)}
	for i := 0; i < count.PageCount; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := p.parsePage(ctx, inst, doc.Document, i)
		if err != nil {
			return nil, err
		}
		out.Pages = append(out.Pages, page)
	}
	return out, nil
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

	// Scanned/image-only page: no extractable text. Fall back to OCR if set.
	if len(page.Words) == 0 {
		ocrWords, err := p.ocrPage(ctx, inst, pageReq, pageSize{Width: size.Width, Height: size.Height})
		if err != nil {
			return Page{}, err
		}
		page.Words = ocrWords
	}
	return page, nil
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
