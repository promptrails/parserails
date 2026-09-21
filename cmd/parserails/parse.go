package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/promptrails/parserails"
)

func cmdParse(args []string) error {
	fs := flag.NewFlagSet("parse", flag.ExitOnError)
	format := fs.String("format", "text", "output format: text|json|markdown")
	asJSON := fs.Bool("json", false, "shorthand for -format json")
	gran := fs.String("granularity", "word", "segmentation: word|line")
	font := fs.Bool("font", false, "collect font sizes (word granularity)")
	pages := fs.String("pages", "", `pages to parse, 1-based (e.g. "1-5,10")`)
	maxPages := fs.Int("max-pages", 0, "cap how many pages are parsed")
	keepFurniture := fs.Bool("keep-headers-footers", false,
		"keep running headers and footers in markdown output")
	common := addCommonFlags(fs)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: parserails parse [flags] <file>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := oneFileArg(fs); err != nil {
		return err
	}
	if *asJSON {
		*format = "json"
	}
	switch *format {
	case "text", "json", "markdown":
	default:
		return fmt.Errorf("invalid -format %q (want text|json|markdown)", *format)
	}

	var extra []parserails.Option
	switch strings.ToLower(*gran) {
	case "word":
	case "line":
		extra = append(extra, parserails.WithGranularity(parserails.GranularityLine))
	default:
		return fmt.Errorf("invalid -granularity %q (want word|line)", *gran)
	}
	if *font || *format == "markdown" {
		// Markdown ranks headings by size, so it always wants font metrics.
		extra = append(extra, parserails.WithFontInfo())
	}
	if *format == "markdown" {
		extra = append(extra, parserails.WithImages())
	}
	opts, err := common.options(extra...)
	if err != nil {
		return err
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

	// -native-office means "do not call LibreOffice". Honour it here rather
	// than handing the package to ParseData, which always converts: text and
	// Markdown do not need a page layout, and demanding LibreOffice for them
	// is exactly what the flag asks us not to do.
	if *common.nativeOffice {
		if done, err := writeNativeOffice(data, inputName(fs.Arg(0)), *format); done || err != nil {
			return err
		}
	}

	doc, err := p.ParseData(context.Background(), data, parserails.ReadOptions{
		Name: inputName(fs.Arg(0)), Pages: *pages, MaxPages: *maxPages,
	})
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

// writeNativeOffice renders an OOXML package without LibreOffice, reporting
// whether it handled the request. JSON output is not one it can handle: word
// boxes need a laid-out page, which a package does not have.
func writeNativeOffice(data []byte, name, format string) (bool, error) {
	detected := parserails.Detect(name, data)
	if !detected.IsOOXML() {
		return false, nil
	}
	if format == "json" {
		return false, fmt.Errorf(
			"-format json needs page coordinates, which -native-office cannot produce for %s; "+
				"drop -native-office (LibreOffice) or use -format text|markdown", detected)
	}
	office, err := parserails.ReadOfficeDocument(data, detected)
	if err != nil {
		return false, err
	}
	if format == "markdown" {
		_, err = fmt.Print(office.Markdown())
	} else {
		_, err = fmt.Println(office.Text())
	}
	return true, err
}
