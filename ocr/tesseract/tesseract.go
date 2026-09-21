// Package tesseract is a cgo-free OCR backend for ParseRails that shells out to
// the `tesseract` command-line binary.
//
// It does NOT link libtesseract — it runs the binary as a subprocess and parses
// its TSV output, keeping ParseRails' cgo-free promise. The `tesseract` binary
// must be installed and on PATH (or set Config.Binary).
package tesseract

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os/exec"
	"strconv"
	"strings"

	"github.com/promptrails/parserails"
)

// Config configures the Tesseract backend.
type Config struct {
	Binary        string  // tesseract binary; defaults to "tesseract".
	Lang          string  // -l language(s); defaults to "eng".
	PSM           int     // --psm page segmentation mode; defaults to 3 (auto).
	MinConfidence float64 // drop words below this confidence (0–100); default 0.
}

type backend struct{ cfg Config }

// New returns a Tesseract OCR backend implementing parserails.OCR.
func New(cfg Config) parserails.OCR {
	if cfg.Binary == "" {
		cfg.Binary = "tesseract"
	}
	if cfg.Lang == "" {
		cfg.Lang = "eng"
	}
	if cfg.PSM == 0 {
		cfg.PSM = 3
	}
	return &backend{cfg: cfg}
}

// Recognize encodes the page image to PNG, pipes it to `tesseract ... tsv`, and
// parses the word rows. Boxes are returned in image pixel space (top-left
// origin), per the parserails.OCR contract.
func (b *backend) Recognize(ctx context.Context, img image.Image) ([]parserails.Word, error) {
	var in bytes.Buffer
	if err := png.Encode(&in, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}

	args := []string{
		"-", "stdout",
		"-l", b.cfg.Lang,
		"--psm", strconv.Itoa(b.cfg.PSM),
		"tsv",
	}
	// #nosec G204 -- b.cfg.Binary is the configured tesseract binary and args
	// are built from literals and numeric config, never from document content.
	cmd := exec.CommandContext(ctx, b.cfg.Binary, args...)
	cmd.Stdin = &in
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("run %s: %w: %s", b.cfg.Binary, err, strings.TrimSpace(stderr.String()))
	}
	return b.parseTSV(out.Bytes())
}

// TSV columns: level page block par line word left top width height conf text
const (
	colLevel = 0
	colLeft  = 6
	colTop   = 7
	colWidth = 8
	colHeigh = 9
	colConf  = 10
	colText  = 11
	wordLvl  = 5
)

func (b *backend) parseTSV(data []byte) ([]parserails.Word, error) {
	var words []parserails.Word
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first { // header row
			first = false
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) <= colText {
			continue
		}
		if atoi(f[colLevel]) != wordLvl {
			continue
		}
		text := strings.TrimSpace(f[colText])
		if text == "" {
			continue
		}
		conf := atof(f[colConf])
		if conf < b.cfg.MinConfidence {
			continue
		}
		left, top := atof(f[colLeft]), atof(f[colTop])
		width, height := atof(f[colWidth]), atof(f[colHeigh])
		words = append(words, parserails.Word{
			Text: text,
			X0:   left, Y0: top, // pixel space, top-left origin
			X1: left + width, Y1: top + height,
			// Tesseract reports confidence as a percentage; Word uses 0–1.
			Confidence: clampConfidence(conf / 100),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read tsv: %w", err)
	}
	return words, nil
}

func atoi(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

func atof(s string) float64 { f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return f }

// clampConfidence keeps the score inside 0–1. Tesseract reports -1 for rows it
// could not score, and a word that is recognized at all is not 0-confident, so
// unscored words get the smallest positive value rather than looking native.
func clampConfidence(c float64) float64 {
	switch {
	case c <= 0:
		return 0.01
	case c > 1:
		return 1
	}
	return c
}
