package parserails

import (
	"context"
	"fmt"
	"os"
)

// ReadOptions tunes a single read: how the input is identified and which parts
// of it are read. The zero value is valid and means "detect the format from the
// bytes, read everything".
type ReadOptions struct {
	// Name is a file name hint. It is only consulted where the bytes are
	// ambiguous — legacy OLE documents, or content no magic number identifies.
	Name string

	// Password opens an encrypted document. Empty means the parser's default
	// (see WithPassword).
	Password string

	// Pages selects which pages to read, 1-based and inclusive, in the form
	// "1-5,10,15-20". Empty means every page. Pages beyond the end of the
	// document are skipped.
	Pages string

	// MaxPages caps how many pages are read after Pages is applied. Zero means
	// the parser's default (see WithMaxPages); a negative value means no cap.
	MaxPages int
}

// withDefaults fills unset fields from the parser's configuration.
func (p *Parser) withDefaults(opt ReadOptions) ReadOptions {
	if opt.Password == "" {
		opt.Password = p.password
	}
	if opt.MaxPages == 0 {
		opt.MaxPages = p.maxPages
	}
	if opt.MaxPages < 0 {
		opt.MaxPages = 0
	}
	return opt
}

// ParseData parses a document held in memory, detecting its format from the
// bytes (and, where those are ambiguous, from [ReadOptions].Name).
//
// PDFs are parsed directly; office documents are converted with LibreOffice
// first. Use it when a document arrives over the wire, out of an archive or an
// e-mail attachment, where there is no path to hand to ParseFile.
func (p *Parser) ParseData(ctx context.Context, data []byte, opt ReadOptions) (*Document, error) {
	format := Detect(opt.Name, data)
	switch {
	case format == FormatPDF:
		return p.parsePDF(ctx, data, opt)
	case format.IsOffice():
		pdf, err := p.convertDataToPDF(ctx, data, format)
		if err != nil {
			return nil, err
		}
		return p.parsePDF(ctx, pdf, opt)
	case format.IsImage():
		return p.ParseImage(ctx, data)
	default:
		return nil, unparsableError(opt.Name, format)
	}
}

// ParseFile reads and parses a document from disk. PDFs are parsed directly;
// office documents (DOCX/PPTX/XLSX/...) are first converted to PDF via
// LibreOffice. The format comes from the file's content, not its extension.
func (p *Parser) ParseFile(ctx context.Context, path string) (*Document, error) {
	data, format, err := readAndDetect(path)
	if err != nil {
		return nil, err
	}
	opt := ReadOptions{Name: path}
	switch {
	case format == FormatPDF:
		return p.parsePDF(ctx, data, opt)
	case format.IsOffice():
		// Convert from the path we already have rather than a temp copy.
		pdf, err := p.convertToPDF(ctx, path)
		if err != nil {
			return nil, err
		}
		return p.parsePDF(ctx, pdf, opt)
	case format.IsImage():
		return p.ParseImage(ctx, data)
	default:
		return nil, unparsableError(path, format)
	}
}

// readAndDetect reads a file and identifies its format.
func readAndDetect(path string) ([]byte, Format, error) {
	// #nosec G304 -- `path` is this function's own API parameter: opening the
	// file the caller names is the whole contract of a document parser.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, FormatUnknown, fmt.Errorf("parserails: read file: %w", err)
	}
	return data, Detect(path, data), nil
}

func unparsableError(name string, format Format) error {
	if name == "" {
		return fmt.Errorf("parserails: cannot parse %s content", format)
	}
	return fmt.Errorf("parserails: cannot parse %q: %s content", name, format)
}
