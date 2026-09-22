package main

import (
	"context"
	"flag"
	"fmt"
	"image/png"
	"os"
	"strings"

	"github.com/promptrails/parserails"
)

func cmdRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	page := fs.Int("page", 0, "page index (0-based)")
	dpi := fs.Int("dpi", 150, "render resolution")
	out := fs.String("o", "", "output PNG path (default <file>-p<page>.png)")
	password := fs.String("password", "", "password for encrypted documents")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: parserails render [flags] <file>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := oneFileArg(fs); err != nil {
		return err
	}
	src := fs.Arg(0)

	data, err := readInput(src)
	if err != nil {
		return err
	}
	if format := parserails.Detect(inputName(src), data); format != parserails.FormatPDF {
		return fmt.Errorf("render supports PDF only; %s is %s", src, format)
	}

	p, err := parserails.New()
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	img, err := p.RenderPage(context.Background(), data, parserails.RenderRequest{
		Page: *page, DPI: *dpi, Password: *password,
	})
	if err != nil {
		return err
	}

	dst := *out
	if dst == "" {
		if src == "-" {
			return fmt.Errorf("-o is required when reading from standard input")
		}
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
