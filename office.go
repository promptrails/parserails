package parserails

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNoLibreOffice reports that no LibreOffice binary could be found. Office
// conversion wraps it, so callers can fall back to reading a package natively
// instead of treating a missing dependency like a broken document.
var ErrNoLibreOffice = errors.New("parserails: LibreOffice not found")

// locateSoffice finds the LibreOffice binary: an explicit path, then the
// PARSERAILS_SOFFICE env var, then "soffice"/"libreoffice" on PATH.
func locateSoffice(explicit string) (string, error) {
	candidates := []string{explicit, os.Getenv("PARSERAILS_SOFFICE"), "soffice", "libreoffice"}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w (install it or set PARSERAILS_SOFFICE)", ErrNoLibreOffice)
}

// convertToPDF renders an office document to PDF bytes using headless
// LibreOffice, under the parser's timeout. Unlike PDFium, LibreOffice is a
// subprocess and really is killable, so a stuck conversion dies rather than
// being abandoned.
func (p *Parser) convertToPDF(ctx context.Context, path string) ([]byte, error) {
	return bounded(ctx, p, func(ctx context.Context) ([]byte, error) {
		return p.convertFile(ctx, path)
	})
}

// convertFile is the conversion itself. A throwaway user profile is used per
// call so conversions can run concurrently without colliding on LibreOffice's
// default profile lock.
func (p *Parser) convertFile(ctx context.Context, path string) ([]byte, error) {
	bin, err := locateSoffice(p.sofficeBin)
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "parserails-soffice-")
	if err != nil {
		return nil, fmt.Errorf("parserails: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	profile := filepath.Join(tmp, "profile")
	// #nosec G204 -- bin is the configured LibreOffice binary, not user input;
	// every argument below is a literal or a path this function just created.
	cmd := exec.CommandContext(ctx, bin,
		"--headless", "--norestore",
		"-env:UserInstallation=file://"+profile,
		"--convert-to", "pdf",
		"--outdir", tmp,
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("parserails: libreoffice convert: %w: %s", err, strings.TrimSpace(string(out)))
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	pdfPath := filepath.Join(tmp, base+".pdf")
	// #nosec G304 -- pdfPath is built from a temp directory this function made
	// moments ago; nothing outside chooses it.
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("parserails: read converted pdf: %w", err)
	}
	return data, nil
}

// convertDataToPDF converts an office document held in memory. LibreOffice only
// reads files, so the bytes are staged in a temp file named with the format's
// extension — it dispatches on that, not on content.
func (p *Parser) convertDataToPDF(ctx context.Context, data []byte, format Format) ([]byte, error) {
	dir, err := os.MkdirTemp("", "parserails-input-")
	if err != nil {
		return nil, fmt.Errorf("parserails: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "document"+format.Ext())
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("parserails: stage input: %w", err)
	}
	return p.convertToPDF(ctx, path)
}

// officeText reads an office document's text, natively for an OOXML package
// when that was asked for — or when LibreOffice is missing and the package can
// be read without it.
func (p *Parser) officeText(ctx context.Context, data []byte, format Format, opt ReadOptions) (string, error) {
	if p.nativeOffice && format.IsOOXML() {
		doc, err := ReadOfficeDocument(data, format)
		if err != nil {
			return "", err
		}
		return doc.Text(), nil
	}

	pdf, err := p.convertDataToPDF(ctx, data, format)
	if err != nil {
		if errors.Is(err, ErrNoLibreOffice) && format.IsOOXML() {
			if doc, nativeErr := ReadOfficeDocument(data, format); nativeErr == nil {
				return doc.Text(), nil
			}
		}
		return "", err
	}
	return p.extractPDFText(ctx, pdf, opt)
}
