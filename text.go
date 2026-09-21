package parserails

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/klippa-app/go-pdfium/requests"
)

// ExtractText returns the plain text of every page, joined by form feeds, with
// no bounding boxes. It uses PDFium's whole-page text API — one call per page
// instead of per character — so it is far faster and lighter than Parse. This is
// the right entry point for text-only needs like RAG ingestion or search
// indexing, where layout is not required.
//
// Scanned/image-only pages produce no text here; ExtractText does not run OCR
// (use Parse with WithOCR for that).
func (p *Parser) ExtractText(ctx context.Context, data []byte) (string, error) {
	if f := Sniff(data); f != FormatPDF && f != FormatUnknown {
		return "", fmt.Errorf("parserails: ExtractText wants PDF data, got %s; use ExtractTextData", f)
	}
	return p.extractPDFText(ctx, data, ReadOptions{})
}

// ExtractTextData is the in-memory counterpart of ExtractText: it detects the
// format, converts office documents, and honours ReadOptions (password, page
// selection).
func (p *Parser) ExtractTextData(ctx context.Context, data []byte, opt ReadOptions) (string, error) {
	format := Detect(opt.Name, data)
	switch {
	case format == FormatPDF:
		return p.extractPDFText(ctx, data, opt)
	case format.IsOffice():
		return p.officeText(ctx, data, format, opt)
	case format.IsImage():
		doc, err := p.ParseImage(ctx, data)
		if err != nil {
			return "", err
		}
		return doc.Text(), nil
	default:
		return "", unparsableError(opt.Name, format)
	}
}

func (p *Parser) extractPDFText(ctx context.Context, data []byte, opt ReadOptions) (string, error) {
	opt = p.withDefaults(opt)

	inst, err := p.pool.GetInstance(30 * time.Second)
	if err != nil {
		return "", fmt.Errorf("parserails: acquire instance: %w", err)
	}
	defer func() { _ = inst.Close() }()

	doc, err := openDocument(inst, data, opt.Password)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	}()

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return "", fmt.Errorf("parserails: page count: %w", err)
	}
	pages, err := selectPages(count.PageCount, opt)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for n, i := range pages {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		res, err := inst.GetPageText(&requests.GetPageText{
			Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}},
		})
		if err != nil {
			return "", fmt.Errorf("parserails: page %d text: %w", i, err)
		}
		if n > 0 {
			b.WriteByte('\f')
		}
		b.WriteString(res.Text)
	}
	return b.String(), nil
}

// ExtractFileText is the file counterpart of ExtractText: PDFs are read directly,
// office documents are converted via LibreOffice first.
func (p *Parser) ExtractFileText(ctx context.Context, path string) (string, error) {
	data, format, err := readAndDetect(path)
	if err != nil {
		return "", err
	}
	switch {
	case format == FormatPDF:
		return p.extractPDFText(ctx, data, ReadOptions{Name: path})
	case format.IsOffice():
		return p.officeText(ctx, data, format, ReadOptions{Name: path})
	case format.IsImage():
		doc, err := p.ParseImage(ctx, data)
		if err != nil {
			return "", err
		}
		return doc.Text(), nil
	default:
		return "", unparsableError(path, format)
	}
}
