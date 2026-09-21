package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/promptrails/parserails"
)

func cmdBatch(args []string) error {
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	format := fs.String("format", "text", "output format: text|json|markdown")
	ext := fs.String("ext", "", "only process files with this extension (e.g. .pdf)")
	recursive := fs.Bool("recursive", true, "descend into subdirectories")
	concurrency := fs.Int("concurrency", runtime.NumCPU(), "documents parsed at once")
	quiet := fs.Bool("q", false, "only report failures")
	common := addCommonFlags(fs)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: parserails batch [flags] <input-dir> <output-dir>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return fmt.Errorf("expected an input directory and an output directory")
	}
	inDir, outDir := fs.Arg(0), fs.Arg(1)

	suffix, err := outputSuffix(*format)
	if err != nil {
		return err
	}
	extra := []parserails.Option{}
	if *format == "markdown" {
		extra = append(extra, parserails.WithFontInfo(), parserails.WithImages())
	}
	opts, err := common.options(extra...)
	if err != nil {
		return err
	}

	inputs, err := collectInputs(inDir, *ext, *recursive)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return fmt.Errorf("no documents found in %s", inDir)
	}

	p, err := parserails.New(opts...)
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()

	// Bounded concurrency over the whole batch; one document's failure is
	// reported and the rest keep going.
	var (
		wg     sync.WaitGroup
		sem    = make(chan struct{}, max(*concurrency, 1))
		mu     sync.Mutex
		failed int
	)
	for _, in := range inputs {
		wg.Add(1)
		sem <- struct{}{}
		go func(in string) {
			defer wg.Done()
			defer func() { <-sem }()

			rel, err := filepath.Rel(inDir, in)
			if err != nil {
				rel = filepath.Base(in)
			}
			out := filepath.Join(outDir, strings.TrimSuffix(rel, filepath.Ext(rel))+suffix)
			err = convertOne(p, in, out, *format)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				failed++
				fmt.Fprintf(os.Stderr, "%s: %v\n", rel, err)
			case !*quiet:
				fmt.Println(out)
			}
		}(in)
	}
	wg.Wait()

	if !*quiet {
		fmt.Fprintf(os.Stderr, "%d/%d document(s) written\n", len(inputs)-failed, len(inputs))
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d document(s) failed", failed, len(inputs))
	}
	return nil
}

func convertOne(p *parserails.Parser, in, out, format string) error {
	doc, err := p.ParseFile(context.Background(), in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return err
	}
	// #nosec G304 -- out is derived from the output directory the operator gave.
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	switch format {
	case "json":
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	case "markdown":
		_, err = f.WriteString(doc.Markdown())
	default:
		_, err = f.WriteString(doc.Text())
	}
	return err
}

func outputSuffix(format string) (string, error) {
	switch format {
	case "text":
		return ".txt", nil
	case "json":
		return ".json", nil
	case "markdown":
		return ".md", nil
	default:
		return "", fmt.Errorf("invalid -format %q (want text|json|markdown)", format)
	}
}

// collectInputs lists the documents to parse, skipping anything ParseRails
// has no reader for rather than failing on it later.
func collectInputs(dir, ext string, recursive bool) ([]string, error) {
	var out []string
	ext = strings.ToLower(ext)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if !recursive && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if ext != "" && strings.ToLower(filepath.Ext(path)) != ext {
			return nil
		}
		format := parserails.FormatByName(path)
		if format != parserails.FormatPDF && !format.IsOffice() && !format.IsImage() {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out, err
}
