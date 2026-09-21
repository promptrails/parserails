// Command parserails is a CLI for the ParseRails document parser.
//
//	go install github.com/promptrails/parserails/cmd/parserails@latest
//
//	parserails parse      [flags] <file>   extract spatial text (plain or JSON)
//	parserails render     [flags] <file>   render a page to a PNG image
//	parserails extract    [flags] <file>   walk a file and everything inside it
//	parserails is-complex [flags] <file>   report which pages need OCR
//	parserails version                     print version
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
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
	case "extract":
		err = cmdExtract(os.Args[2:])
	case "is-complex":
		err = cmdIsComplex(os.Args[2:])
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
		var verdict complexVerdict
		if errors.As(err, &verdict) {
			os.Exit(verdict.code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// complexVerdict carries is-complex's exit code out of the command without
// pretending the document was an error.
type complexVerdict struct{ code int }

func (complexVerdict) Error() string { return "document needs OCR" }

func usage() {
	fmt.Fprint(os.Stderr, `parserails — fast, cgo-free document parsing

usage:
  parserails parse      [flags] <file>   extract spatial text (PDF or office doc)
  parserails render     [flags] <file>   render a page to PNG
  parserails extract    [flags] <file>   walk a file and everything inside it
  parserails is-complex [flags] <file>   report which pages need OCR
  parserails version

run "parserails <command> -h" for flags
`)
}

func cmdParse(args []string) error {
	fs := flag.NewFlagSet("parse", flag.ExitOnError)
	format := fs.String("format", "text", "output format: text|json|markdown")
	asJSON := fs.Bool("json", false, "shorthand for -format json")
	gran := fs.String("granularity", "word", "segmentation: word|line")
	font := fs.Bool("font", false, "collect font sizes (word granularity)")
	ocrName := fs.String("ocr", "none", "OCR for scanned pages: none|tesseract")
	lang := fs.String("lang", "eng", "OCR language")
	keepFurniture := fs.Bool("keep-headers-footers", false,
		"keep running headers and footers in markdown output")
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
	if *asJSON {
		*format = "json"
	}
	switch *format {
	case "text", "json", "markdown":
	default:
		return fmt.Errorf("invalid -format %q (want text|json|markdown)", *format)
	}

	opts := []parserails.Option{}
	switch strings.ToLower(*gran) {
	case "word":
	case "line":
		opts = append(opts, parserails.WithGranularity(parserails.GranularityLine))
	default:
		return fmt.Errorf("invalid -granularity %q (want word|line)", *gran)
	}
	if *font || *format == "markdown" {
		// Markdown ranks headings by size, so it always wants font metrics.
		opts = append(opts, parserails.WithFontInfo())
	}
	if *format == "markdown" {
		opts = append(opts, parserails.WithImages())
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

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	case "markdown":
		_, err = fmt.Print(doc.MarkdownWith(parserails.BlockOptions{
			KeepHeadersFooters: *keepFurniture,
		}))
		return err
	default:
		_, err = fmt.Println(doc.Text())
		return err
	}
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

	// #nosec G304 -- src is the path the operator typed on the command line.
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
	// #nosec G304 -- dst is the output path the operator asked for.
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
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one input file")
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
		Name: fs.Arg(0), Password: *password, Pages: *pages, MaxPages: *maxPages,
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

// readInput reads a file, or standard input when the path is "-".
func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	// #nosec G304 -- path is what the operator typed on the command line.
	return os.ReadFile(path)
}

func cmdExtract(args []string) error {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	format := fs.String("format", "tree", "output: tree|text|json")
	maxDepth := fs.Int("max-depth", 0, "how deep to descend (0 = default 8, -1 = no limit)")
	maxFiles := fs.Int("max-files", 0, "cap how many files are opened (0 = default 512)")
	ocrName := fs.String("ocr", "none", "OCR for scanned pages and images: none|tesseract")
	lang := fs.String("lang", "eng", "OCR language")
	password := fs.String("password", "", "password for encrypted documents")
	skipParse := fs.Bool("list", false, "only list what is inside; do not parse it")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: parserails extract [flags] <file>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one input file")
	}

	var opts []parserails.Option
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

	data, err := readInput(fs.Arg(0))
	if err != nil {
		return err
	}
	node, err := p.Extract(context.Background(), data, parserails.ExtractOptions{
		ReadOptions: parserails.ReadOptions{Name: filepath.Base(fs.Arg(0)), Password: *password},
		MaxDepth:    *maxDepth,
		MaxFiles:    *maxFiles,
		SkipParse:   *skipParse,
	})
	if err != nil {
		return err
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(node)
	case "text":
		_, err = fmt.Println(node.AllText())
		return err
	case "tree":
		printTree(node, 0)
		return nil
	default:
		return fmt.Errorf("invalid -format %q (want tree|text|json)", *format)
	}
}

// printTree renders the extraction tree the way `tree` renders a directory.
func printTree(n *parserails.Node, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s%s  [%s]", indent, n.Name, n.Format)
	switch {
	case n.Err != nil:
		fmt.Printf("  error: %v", n.Err)
	case n.Document != nil:
		fmt.Printf("  %d page(s)", len(n.Document.Pages))
	case n.Text != "":
		fmt.Printf("  %d char(s)", len(n.Text))
	}
	fmt.Println()
	for _, c := range n.Children {
		printTree(c, depth+1)
	}
}
