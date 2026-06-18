// Package httpocr is an OCR backend for ParseRails that delegates to a remote
// HTTP OCR server (EasyOCR/PaddleOCR-style). It posts the page image and decodes
// a JSON response of words with pixel-space bounding boxes.
//
// The server contract:
//
//	POST <URL>           body: image/png bytes
//	200 OK               body: {"words":[{"text":"hi","x0":1,"y0":2,"x1":3,"y1":4}]}
//
// Boxes are in image pixel space (top-left origin), per the parserails.OCR
// contract; ParseRails maps them back to PDF points.
package httpocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"time"

	"github.com/promptrails/parserails"
)

// Config configures the HTTP OCR backend.
type Config struct {
	URL    string       // OCR endpoint; required.
	Client *http.Client // defaults to a client with a 60s timeout.
	Header http.Header  // optional headers (auth, etc.) sent with each request.
}

type backend struct{ cfg Config }

// New returns an HTTP OCR backend implementing parserails.OCR.
func New(cfg Config) parserails.OCR {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 60 * time.Second}
	}
	return &backend{cfg: cfg}
}

type wordsResponse struct {
	Words []struct {
		Text string  `json:"text"`
		X0   float64 `json:"x0"`
		Y0   float64 `json:"y0"`
		X1   float64 `json:"x1"`
		Y1   float64 `json:"y1"`
	} `json:"words"`
}

// Recognize posts the image to the configured endpoint and decodes the words.
func (b *backend) Recognize(ctx context.Context, img image.Image) ([]parserails.Word, error) {
	if b.cfg.URL == "" {
		return nil, fmt.Errorf("httpocr: URL is required")
	}
	var body bytes.Buffer
	if err := png.Encode(&body, img); err != nil {
		return nil, fmt.Errorf("httpocr: encode png: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.URL, &body)
	if err != nil {
		return nil, fmt.Errorf("httpocr: new request: %w", err)
	}
	req.Header.Set("Content-Type", "image/png")
	for k, vs := range b.cfg.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := b.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpocr: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("httpocr: server returned %s", resp.Status)
	}

	var out wordsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("httpocr: decode response: %w", err)
	}

	words := make([]parserails.Word, 0, len(out.Words))
	for _, w := range out.Words {
		if w.Text == "" {
			continue
		}
		words = append(words, parserails.Word{
			Text: w.Text, X0: w.X0, Y0: w.Y0, X1: w.X1, Y1: w.Y1,
		})
	}
	return words, nil
}
