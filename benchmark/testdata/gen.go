//go:build ignore

// gen.go generates the sample PDFs used by the benchmarks. It uses only the Go
// standard library so it never touches the benchmark module's dependency graph.
//
//	go run gen.go
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	write("small.pdf", buildPDF(1, smallLines))
	write("report.pdf", buildPDF(8, reportLines))
}

func write(name string, data []byte) {
	if err := os.WriteFile(name, data, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", name, len(data))
}

var smallLines = []string{
	"ParseRails Benchmark Document",
	"",
	"This is a small single page PDF used to compare text extraction",
	"throughput across Go PDF libraries. The text below is plain ASCII",
	"laid out as discrete lines so every reader can extract it.",
	"",
	"Invoice number 10042 dated 2026 06 18 total amount 1499 USD.",
	"Customer Acme Corporation billing department net 30 terms.",
	"Line item one widget quantity 12 unit price 49 subtotal 588.",
	"Line item two gadget quantity 7 unit price 130 subtotal 910.",
	"Thank you for your business and please remit payment promptly.",
}

func reportLines(page int) []string {
	out := []string{
		fmt.Sprintf("Quarterly Report Section %d", page+1),
		"",
	}
	for i := 0; i < 34; i++ {
		out = append(out, fmt.Sprintf(
			"Row %02d figures revenue %d cost %d margin %d percent across regions.",
			i, 1000+i*7, 400+i*3, 30+i%20))
	}
	return out
}

// buildPDF writes a valid multi page PDF with the given per page line producer.
func buildPDF(pages int, lines interface{}) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", // 1
		"",                                  // 2 (pages, filled below)
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", // 3
	}

	kids := make([]string, 0, pages)
	for p := 0; p < pages; p++ {
		pageObj := 4 + 2*p
		contentObj := 5 + 2*p
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObj))

		var pl []string
		switch f := lines.(type) {
		case []string:
			pl = f
		case func(int) []string:
			pl = f(p)
		}
		content := contentStream(pl)

		objects = append(objects, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 3 0 R >> >> >>",
			contentObj))
		objects = append(objects, fmt.Sprintf(
			"<< /Length %d >>\nstream\n%sendstream", len(content), content))
	}

	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>",
		strings.Join(kids, " "), pages)

	return assemble(objects)
}

func contentStream(lines []string) string {
	var b strings.Builder
	b.WriteString("BT\n/F1 12 Tf\n1 0 0 1 72 740 Tm\n14 TL\n")
	for _, ln := range lines {
		fmt.Fprintf(&b, "(%s) Tj\nT*\n", escape(ln))
	}
	b.WriteString("ET\n")
	return b.String()
}

func escape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)")
	return r.Replace(s)
}

func assemble(objects []string) []byte {
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, body := range objects {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", len(objects)+1)
	b.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF",
		len(objects)+1, xref)
	return []byte(b.String())
}
