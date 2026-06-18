// Command parserails is a CLI for the ParseRails document parser.
//
//	go install github.com/promptrails/parserails/cmd/parserails@latest
//
//	parserails parse   [flags] <file>   extract spatial text (plain or JSON)
//	parserails render  [flags] <file>   render a page to a PNG image
//	parserails version                  print version
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"os"
	"runtime/debug"
	"strings"

	"github.com/promptrails/parserails"
	"github.com/promptrails/parserails/ocr/tesseract"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "parse":
		err = cmdParse(os.Args[2:])
	case "render":
		err = cmdRender(os.Args[2:])
	case "version":
		fmt.Println(version())
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `parserails — fast, cgo-free document parsing

usage:
  parserails parse  [flags] <file>   extract spatial text (PDF or office doc)
  parserails render [flags] <file>   render a page to PNG
  parserails version

run "parserails parse -h" or "parserails render -h" for flags
`)
}

func cmdParse(args []string) error {
	fs := flag.NewFlagSet("parse", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output JSON instead of plain text")
	gran := fs.String("granularity", "word", "segmentation: word|line")
	font := fs.Bool("font", false, "collect font sizes (word granularity)")
	ocrName := fs.String("ocr", "none", "OCR for scanned pages: none|tesseract")
	lang := fs.String("lang", "eng", "OCR language")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: parserails parse [flags] <file>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one input file")
	}

	opts := []parserails.Option{}
	switch strings.ToLower(*gran) {
	case "word":
	case "line":
		opts = append(opts, parserails.WithGranularity(parserails.GranularityLine))
	default:
		return fmt.Errorf("invalid -granularity %q (want word|line)", *gran)
	}
	if *font {
		opts = append(opts, parserails.WithFontInfo())
	}
	switch strings.ToLower(*ocrName) {
	case "none":
	case "tesseract":
		opts = append(opts, parserails.WithOCR(tesseract.New(tesseract.Config{Lang: *lang})))
	default:
		return fmt.Errorf("invalid -ocr %q (want none|tesseract)", *ocrName)
	}

	p, err := parserails.New(opts...)
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	doc, err := p.ParseFile(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	}
	for _, pg := range doc.Pages {
		for _, w := range pg.Words {
			fmt.Println(w.Text)
		}
	}
	return nil
}

func cmdRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	page := fs.Int("page", 0, "page index (0-based)")
	dpi := fs.Int("dpi", 150, "render resolution")
	out := fs.String("o", "", "output PNG path (default <file>-p<page>.png)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: parserails render [flags] <file>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one input file")
	}
	src := fs.Arg(0)
	if parserails.IsOfficeFormat(src) {
		return fmt.Errorf("render supports PDF only; convert %q first", src)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	p, err := parserails.New()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	img, err := p.RenderPage(context.Background(), data, parserails.RenderRequest{Page: *page, DPI: *dpi})
	if err != nil {
		return err
	}

	dst := *out
	if dst == "" {
		dst = fmt.Sprintf("%s-p%d.png", strings.TrimSuffix(src, ".pdf"), *page)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Println(dst)
	return nil
}

func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return "parserails " + info.Main.Version
	}
	return "parserails (dev)"
}
