// Package benchmark compares ParseRails against other Go PDF libraries.
//
// It is a separate Go module (see go.mod) on purpose: the competitors pull in
// large, sometimes AGPL/commercial dependencies, and keeping them here means the
// main parserails module's dependency graph stays clean. Run with:
//
//	cd benchmark && go test -bench=. -benchmem ./...
package benchmark

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	ledong "github.com/ledongthuc/pdf"
	pdfcpu "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/promptrails/parserails"
	uniextractor "github.com/unidoc/unipdf/v3/extractor"
	unimodel "github.com/unidoc/unipdf/v3/model"
)

// docs are the sample inputs. Regenerate them with: go run testdata/gen.go
var docs = []string{"small.pdf", "report.pdf"}

func load(tb testing.TB, name string) []byte {
	tb.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

// BenchmarkParseRails — PDFium (WASM) spatial extraction, this project.
// Note: this is the only library here that also returns per-word bounding boxes.
func BenchmarkParseRails(b *testing.B) {
	p, err := parserails.New()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = p.Close() })

	for _, name := range docs {
		data := load(b, name)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				doc, err := p.Parse(context.Background(), data)
				if err != nil {
					b.Fatal(err)
				}
				_ = doc.Text()
			}
		})
	}
}

// BenchmarkParseRailsText — plain-text-only fast path (no boxes), the
// apples-to-apples comparison with the pure-text readers below.
func BenchmarkParseRailsText(b *testing.B) {
	p, err := parserails.New()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = p.Close() })

	for _, name := range docs {
		data := load(b, name)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := p.ExtractText(context.Background(), data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkParseRailsLine — same engine, line/rect granularity (fast mode).
func BenchmarkParseRailsLine(b *testing.B) {
	p, err := parserails.New(parserails.WithGranularity(parserails.GranularityLine))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = p.Close() })

	for _, name := range docs {
		data := load(b, name)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				doc, err := p.Parse(context.Background(), data)
				if err != nil {
					b.Fatal(err)
				}
				_ = doc.Text()
			}
		})
	}
}

// BenchmarkLedongthuc — pure-Go reader, plain text (no positions).
func BenchmarkLedongthuc(b *testing.B) {
	for _, name := range docs {
		data := load(b, name)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				r, err := ledong.NewReader(bytes.NewReader(data), int64(len(data)))
				if err != nil {
					b.Fatal(err)
				}
				tr, err := r.GetPlainText()
				if err != nil {
					b.Fatal(err)
				}
				if _, err := io.Copy(io.Discard, tr); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPdfcpu — pure-Go processor. Caveat: pdfcpu has NO plain-text
// extraction. ExtractContent returns the raw, decoded content streams (PDF
// operators), not readable text — so this measures a lighter, different
// operation. Included for reference, not as a text-extraction equivalent.
func BenchmarkPdfcpu(b *testing.B) {
	for _, name := range docs {
		data := load(b, name)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				err := pdfcpu.ExtractContent(bytes.NewReader(data), nil,
					func(r io.Reader, _ int) error {
						_, e := io.Copy(io.Discard, r)
						return e
					}, nil)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkUnipdf — commercial/AGPL reader, plain text (no positions).
// unipdf requires a license key for many operations; if extraction is not
// licensed in this environment the benchmark is skipped rather than failed.
func BenchmarkUnipdf(b *testing.B) {
	for _, name := range docs {
		data := load(b, name)

		if err := unipdfExtract(data); err != nil {
			b.Skipf("unipdf unavailable (likely unlicensed): %v", err)
		}

		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := unipdfExtract(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func unipdfExtract(data []byte) error {
	r, err := unimodel.NewPdfReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	n, err := r.GetNumPages()
	if err != nil {
		return err
	}
	for pg := 1; pg <= n; pg++ {
		page, err := r.GetPage(pg)
		if err != nil {
			return err
		}
		ex, err := uniextractor.New(page)
		if err != nil {
			return err
		}
		if _, err := ex.ExtractText(); err != nil {
			return err
		}
	}
	return nil
}
