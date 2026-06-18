// extract-text is the simplest ParseRails example: open a PDF and print its
// text plus a few word bounding boxes. Pure WASM backend — no cgo, no system
// libraries.
//
//	go run . sample.pdf
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/promptrails/parserails"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: extract-text <file.pdf>")
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

	doc, err := p.Parse(context.Background(), data)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("backend=%s pages=%d words=%d\n\n",
		parserails.Backend, len(doc.Pages), len(doc.Words()))

	words := doc.Words()
	for i, w := range words {
		if i == 8 {
			fmt.Printf("... and %d more words\n", len(words)-8)
			break
		}
		fmt.Printf("p%d %-20q [%.0f %.0f %.0f %.0f]\n", w.Page, w.Text, w.X0, w.Y0, w.X1, w.Y1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
