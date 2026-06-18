// native-cgo shows the high-throughput native backend. Built with
// -tags parserails_cgo it links libpdfium directly instead of running PDFium as
// WebAssembly — the right choice for controlled, performance-critical hosts such
// as a document-ingestion worker.
//
//	go run -tags parserails_cgo . sample.pdf
//
// It prints parserails.Backend (which reports "cgo" in this build) and the
// extracted plain text — the text-only fast path used for RAG/search indexing.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/promptrails/parserails"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: native-cgo <file.pdf>")
		os.Exit(2)
	}

	p, err := parserails.New()
	if err != nil {
		fatal(err)
	}
	defer func() { _ = p.Close() }()

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fatal(err)
	}

	start := time.Now()
	text, err := p.ExtractText(context.Background(), data)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("backend=%s chars=%d elapsed=%s\n\n",
		parserails.Backend, len(text), time.Since(start).Round(time.Microsecond))
	fmt.Println(text)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
