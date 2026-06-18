package parserails

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// officeExts are the formats converted to PDF via LibreOffice before parsing.
var officeExts = map[string]bool{
	".docx": true, ".doc": true,
	".pptx": true, ".ppt": true,
	".xlsx": true, ".xls": true,
	".odt": true, ".odp": true, ".ods": true, ".rtf": true,
}

// IsOfficeFormat reports whether path has an extension ParseRails converts to PDF
// via LibreOffice before parsing.
func IsOfficeFormat(path string) bool {
	return officeExts[strings.ToLower(filepath.Ext(path))]
}

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
	return "", fmt.Errorf("parserails: LibreOffice not found (install it or set PARSERAILS_SOFFICE)")
}

// convertToPDF renders an office document to PDF bytes using headless
// LibreOffice. A throwaway user profile is used per call so conversions can run
// concurrently without colliding on LibreOffice's default profile lock.
func (p *Parser) convertToPDF(ctx context.Context, path string) ([]byte, error) {
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
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("parserails: read converted pdf: %w", err)
	}
	return data, nil
}
