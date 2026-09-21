package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/promptrails/parserails"
)

func cmdIsComplex(args []string) error {
	fs := flag.NewFlagSet("is-complex", flag.ExitOnError)
	compact := fs.Bool("compact", false, "emit dense JSON instead of indented")
	pages := fs.String("pages", "", `pages to inspect, 1-based (e.g. "1-5,10")`)
	maxPages := fs.Int("max-pages", 0, "cap how many pages are inspected")
	password := fs.String("password", "", "password for encrypted documents")
	quiet := fs.Bool("q", false, "suppress the verdict on stderr")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: parserails is-complex [flags] <file>")
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nexit codes: 0 simple, 2 needs OCR, 1 error")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := oneFileArg(fs); err != nil {
		return err
	}

	p, err := parserails.New()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	data, err := readInput(fs.Arg(0))
	if err != nil {
		return err
	}
	result, err := p.Inspect(context.Background(), data, parserails.ReadOptions{
		Name: inputName(fs.Arg(0)), Password: *password, Pages: *pages, MaxPages: *maxPages,
	})
	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	if !*compact {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(result.Pages); err != nil {
		return err
	}

	ocrPages := result.OCRPages()
	if !*quiet {
		verdict := "SIMPLE"
		if len(ocrPages) > 0 {
			verdict = "COMPLEX"
		}
		fmt.Fprintf(os.Stderr, "%s — %d/%d page(s) need OCR\n", verdict, len(ocrPages), len(result.Pages))
	}
	if len(ocrPages) > 0 {
		return complexVerdict{code: 2}
	}
	return nil
}
