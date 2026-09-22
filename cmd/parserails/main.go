// Command parserails is a CLI for the ParseRails document parser.
//
//	go install github.com/promptrails/parserails/cmd/parserails@latest
//
//	parserails parse      [flags] <file>        text, JSON or Markdown
//	parserails batch      [flags] <in> <out>    parse a directory, concurrently
//	parserails extract    [flags] <file>        walk a file and everything inside it
//	parserails render     [flags] <file>        render a page to a PNG image
//	parserails is-complex [flags] <file>        report which pages need OCR
//	parserails version                          print version
//
// Every command that takes a file accepts "-" to read standard input.
package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
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
	case "batch":
		err = cmdBatch(os.Args[2:])
	case "extract":
		err = cmdExtract(os.Args[2:])
	case "render":
		err = cmdRender(os.Args[2:])
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
  parserails parse      [flags] <file>       extract text, JSON or Markdown
  parserails batch      [flags] <in> <out>   parse a directory, concurrently
  parserails extract    [flags] <file>       walk a file and everything inside it
  parserails render     [flags] <file>       render a page to PNG
  parserails is-complex [flags] <file>       report which pages need OCR
  parserails version

"-" as the input file reads standard input.
run "parserails <command> -h" for flags
`)
}

func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return "parserails " + info.Main.Version
	}
	return "parserails (dev)"
}
