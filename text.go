package parserails

import (
	"context"
	"fmt"
	"os"
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
	inst, err := p.pool.GetInstance(30 * time.Second)
	if err != nil {
		return "", fmt.Errorf("parserails: acquire instance: %w", err)
	}
	defer func() { _ = inst.Close() }()

	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return "", fmt.Errorf("parserails: open document: %w", err)
	}
	defer func() {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	}()

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return "", fmt.Errorf("parserails: page count: %w", err)
	}

	var b strings.Builder
	for i := 0; i < count.PageCount; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		res, err := inst.GetPageText(&requests.GetPageText{
			Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}},
		})
		if err != nil {
			return "", fmt.Errorf("parserails: page %d text: %w", i, err)
		}
		if i > 0 {
			b.WriteByte('\f')
		}
		b.WriteString(res.Text)
	}
	return b.String(), nil
}

// ExtractFileText is the file counterpart of ExtractText: PDFs are read directly,
// office documents are converted via LibreOffice first.
func (p *Parser) ExtractFileText(ctx context.Context, path string) (string, error) {
	if IsOfficeFormat(path) {
		pdf, err := p.convertToPDF(ctx, path)
		if err != nil {
			return "", err
		}
		return p.ExtractText(ctx, pdf)
	}
	// #nosec G304 -- `path` is this function's own API parameter: opening the
	// file the caller names is the whole contract of a document parser.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("parserails: read file: %w", err)
	}
	return p.ExtractText(ctx, data)
}
