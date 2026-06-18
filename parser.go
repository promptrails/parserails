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

// Parser extracts spatial text from documents using a pooled PDFium runtime.
//
// It is safe for concurrent use: each Parse call borrows an instance from the
// pool and returns it when done. Create one Parser per process and reuse it.
type Parser struct {
	pool pdfium.Pool
	ocr  OCR
}

// Option configures a Parser.
type Option func(*config)

type config struct {
	minIdle, maxIdle, maxTotal int
	ocr                        OCR
}

// WithOCR sets the OCR backend used as a fallback for pages with no extractable
// text. By default no OCR is performed.
func WithOCR(o OCR) Option { return func(c *config) { c.ocr = o } }

// WithPoolSize tunes the underlying PDFium worker pool.
func WithPoolSize(minIdle, maxIdle, maxTotal int) Option {
	return func(c *config) { c.minIdle, c.maxIdle, c.maxTotal = minIdle, maxIdle, maxTotal }
}

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
	return &Parser{pool: pool, ocr: cfg.ocr}, nil
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
		page, err := p.parsePage(inst, doc.Document, i)
		if err != nil {
			return nil, err
		}
		out.Pages = append(out.Pages, page)
	}
	return out, nil
}

func (p *Parser) parsePage(inst pdfium.Pdfium, docRef references.FPDF_DOCUMENT, index int) (Page, error) {
	pageReq := requests.Page{ByIndex: &requests.PageByIndex{Document: docRef, Index: index}}

	size, err := inst.GetPageSize(&requests.GetPageSize{Page: pageReq})
	if err != nil {
		return Page{}, fmt.Errorf("parserails: page %d size: %w", index, err)
	}

	text, err := inst.GetPageTextStructured(&requests.GetPageTextStructured{
		Page:                   pageReq,
		Mode:                   requests.GetPageTextStructuredModeChars,
		CollectFontInformation: true,
	})
	if err != nil {
		return Page{}, fmt.Errorf("parserails: page %d text: %w", index, err)
	}

	return Page{
		Index:  index,
		Width:  size.Width,
		Height: size.Height,
		Words:  wordsFromChars(text.Chars, index),
	}, nil
}

// chars are grouped into words by splitting on whitespace; the word's box is the
// union of its characters' boxes.
func wordsFromChars(chars []*responses.GetPageTextStructuredChar, page int) []Word {
	var (
		words []Word
		cur   *Word
	)
	flush := func() {
		if cur != nil && cur.Text != "" {
			words = append(words, *cur)
		}
		cur = nil
	}
	for _, c := range chars {
		if strings.TrimSpace(c.Text) == "" {
			flush()
			continue
		}
		pos := c.PointPosition
		if cur == nil {
			cur = &Word{Page: page, X0: pos.Left, Y0: pos.Bottom, X1: pos.Right, Y1: pos.Top}
			if c.FontInformation != nil {
				cur.FontSize = c.FontInformation.Size
			}
		} else {
			cur.X0 = min(cur.X0, pos.Left)
			cur.Y0 = min(cur.Y0, pos.Bottom)
			cur.X1 = max(cur.X1, pos.Right)
			cur.Y1 = max(cur.Y1, pos.Top)
		}
		cur.Text += c.Text
	}
	flush()
	return words
}
