package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/promptrails/parserails"
)

func cmdExtract(args []string) error {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	format := fs.String("format", "tree", "output: tree|text|json")
	maxDepth := fs.Int("max-depth", 0, "how deep to descend (0 = default 8, -1 = no limit)")
	maxFiles := fs.Int("max-files", 0, "cap how many files are opened (0 = default 512)")
	list := fs.Bool("list", false, "only list what is inside; do not parse it")
	common := addCommonFlags(fs)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: parserails extract [flags] <file>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := oneFileArg(fs); err != nil {
		return err
	}
	opts, err := common.options()
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
	name := ""
	if fs.Arg(0) != "-" {
		name = filepath.Base(fs.Arg(0))
	}
	node, err := p.Extract(context.Background(), data, parserails.ExtractOptions{
		ReadOptions: parserails.ReadOptions{Name: name},
		MaxDepth:    *maxDepth,
		MaxFiles:    *maxFiles,
		SkipParse:   *list,
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
	fmt.Printf("%s%s  [%s]", strings.Repeat("  ", depth), n.Name, n.Format)
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
