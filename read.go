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
}

// ParseData parses a document held in memory, detecting its format from the
// bytes (and, where those are ambiguous, from ReadOptions.Name).
//
// PDFs are parsed directly; office documents are converted with LibreOffice
// first. Use it when a document arrives over the wire, out of an archive or an
// e-mail attachment, where there is no path to hand to ParseFile.
func (p *Parser) ParseData(ctx context.Context, data []byte, opt ReadOptions) (*Document, error) {
	format := Detect(opt.Name, data)
	switch {
	case format == FormatPDF:
		return p.Parse(ctx, data)
	case format.IsOffice():
		pdf, err := p.convertDataToPDF(ctx, data, format)
		if err != nil {
			return nil, err
		}
		return p.Parse(ctx, pdf)
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
	switch {
	case format == FormatPDF:
		return p.Parse(ctx, data)
	case format.IsOffice():
		// Convert from the path we already have rather than a temp copy.
		pdf, err := p.convertToPDF(ctx, path)
		if err != nil {
			return nil, err
		}
		return p.Parse(ctx, pdf)
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
