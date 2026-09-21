package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/promptrails/parserails"
	"github.com/promptrails/parserails/ocr/httpocr"
	"github.com/promptrails/parserails/ocr/tesseract"
)

// commonFlags are the options every command that opens a document shares.
type commonFlags struct {
	ocr          *string
	ocrURL       *string
	lang         *string
	imageOCR     *bool
	nativeOffice *bool
	password     *string
	timeout      *time.Duration
}

func addCommonFlags(fs *flag.FlagSet) *commonFlags {
	return &commonFlags{
		ocr:          fs.String("ocr", "none", "OCR backend: none|tesseract|http"),
		ocrURL:       fs.String("ocr-url", "", "OCR server URL (with -ocr http)"),
		lang:         fs.String("lang", "eng", "OCR language"),
		imageOCR:     fs.Bool("image-ocr", false, "also OCR figures on pages that have text"),
		nativeOffice: fs.Bool("native-office", false, "read DOCX/XLSX/PPTX without LibreOffice"),
		password:     fs.String("password", "", "password for encrypted documents"),
		timeout:      fs.Duration("timeout", 0, "give up on a document after this long (0 = no limit)"),
	}
}

// options turns the flags into parser options.
func (c *commonFlags) options(extra ...parserails.Option) ([]parserails.Option, error) {
	opts := extra
	switch strings.ToLower(*c.ocr) {
	case "none":
	case "tesseract":
		opts = append(opts, parserails.WithOCR(tesseract.New(tesseract.Config{Lang: *c.lang})))
	case "http":
		if *c.ocrURL == "" {
			return nil, fmt.Errorf("-ocr http needs -ocr-url")
		}
		opts = append(opts, parserails.WithOCR(httpocr.New(httpocr.Config{
			URL: *c.ocrURL, Language: *c.lang,
		})))
	default:
		return nil, fmt.Errorf("invalid -ocr %q (want none|tesseract|http)", *c.ocr)
	}
	if *c.imageOCR {
		opts = append(opts, parserails.WithImageOCR())
	}
	if *c.nativeOffice {
		opts = append(opts, parserails.WithNativeOffice())
	}
	if *c.password != "" {
		opts = append(opts, parserails.WithPassword(*c.password))
	}
	if *c.timeout > 0 {
		opts = append(opts, parserails.WithTimeout(*c.timeout))
	}
	return opts, nil
}

// readInput reads a file, or standard input when the path is "-", so a
// document can be piped in: curl … | parserails parse -
func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	// #nosec G304 -- path is what the operator typed on the command line.
	return os.ReadFile(path)
}

// inputName is the name a piped document is given for format detection.
func inputName(path string) string {
	if path == "-" {
		return ""
	}
	return path
}

// oneFileArg checks that exactly one input was given.
func oneFileArg(fs *flag.FlagSet) error {
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one input file")
	}
	return nil
}
