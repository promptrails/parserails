// Package httpocr is an OCR backend for ParseRails that delegates to a remote
// HTTP OCR server (EasyOCR, PaddleOCR, RapidOCR, Surya, or your own).
//
// The default wire protocol is the LiteParse OCR API, so the OCR servers
// published for LiteParse work with ParseRails unchanged:
//
//	POST <URL>   multipart/form-data: file=<page.png>, language=<code>
//	200 OK       {"results":[{"text":"hi","bbox":[x1,y1,x2,y2],"confidence":0.95}]}
//
// ProtocolWords keeps ParseRails' original, simpler shape for servers already
// written against it:
//
//	POST <URL>   body: image/png bytes
//	200 OK       {"words":[{"text":"hi","x0":1,"y0":2,"x1":3,"y1":4}]}
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
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	"github.com/promptrails/parserails"
)

// Protocol selects the wire format used to talk to the OCR server.
type Protocol string

const (
	// ProtocolOCRAPI is the LiteParse OCR API specification: a multipart POST
	// with `file` and `language` fields, answered with `results`. It is the
	// default, because it is what the published OCR server images speak.
	ProtocolOCRAPI Protocol = "ocr-api"

	// ProtocolWords posts the raw PNG body and expects a `words` array. This
	// was ParseRails' original protocol.
	ProtocolWords Protocol = "words"
)

// Config configures the HTTP OCR backend.
type Config struct {
	URL      string       // OCR endpoint; required.
	Language string       // language code sent with the request; defaults to "en".
	Protocol Protocol     // wire protocol; defaults to ProtocolOCRAPI.
	Client   *http.Client // defaults to a client with a 60s timeout.
	Header   http.Header  // optional headers (auth, etc.) sent with each request.
}

type backend struct{ cfg Config }

// New returns an HTTP OCR backend implementing parserails.OCR.
func New(cfg Config) parserails.OCR {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.Protocol == "" {
		cfg.Protocol = ProtocolOCRAPI
	}
	if cfg.Language == "" {
		cfg.Language = "en"
	}
	return &backend{cfg: cfg}
}

// ocrResponse accepts either shape: `results` (OCR API) or `words` (legacy).
// Servers are decoded leniently on purpose — a response is data, and a working
// server that answers in the other dialect should not be a configuration error.
type ocrResponse struct {
	Results []struct {
		Text       string      `json:"text"`
		Bbox       []float64   `json:"bbox"`
		Polygon    [][]float64 `json:"polygon"`
		Confidence float64     `json:"confidence"`
	} `json:"results"`
	Words []struct {
		Text       string  `json:"text"`
		X0         float64 `json:"x0"`
		Y0         float64 `json:"y0"`
		X1         float64 `json:"x1"`
		Y1         float64 `json:"y1"`
		Confidence float64 `json:"confidence"`
	} `json:"words"`
}

// Recognize sends the page image to the configured endpoint and decodes the
// recognized words. Boxes are returned in image pixel space (top-left origin).
func (b *backend) Recognize(ctx context.Context, img image.Image) ([]parserails.Word, error) {
	if b.cfg.URL == "" {
		return nil, fmt.Errorf("httpocr: URL is required")
	}

	var raster bytes.Buffer
	if err := encodePNG(&raster, img); err != nil {
		return nil, err
	}

	req, err := b.request(ctx, raster.Bytes())
	if err != nil {
		return nil, err
	}
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

	var out ocrResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("httpocr: decode response: %w", err)
	}
	return out.words(), nil
}

// request builds the HTTP request for the configured protocol.
func (b *backend) request(ctx context.Context, image []byte) (*http.Request, error) {
	if b.cfg.Protocol == ProtocolWords {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.URL, bytes.NewReader(image))
		if err != nil {
			return nil, fmt.Errorf("httpocr: new request: %w", err)
		}
		req.Header.Set("Content-Type", "image/png")
		return req, nil
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="page.png"`)
	header.Set("Content-Type", "image/png")
	part, err := mw.CreatePart(header)
	if err != nil {
		return nil, fmt.Errorf("httpocr: build request: %w", err)
	}
	if _, err := part.Write(image); err != nil {
		return nil, fmt.Errorf("httpocr: build request: %w", err)
	}
	if err := mw.WriteField("language", b.cfg.Language); err != nil {
		return nil, fmt.Errorf("httpocr: build request: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("httpocr: build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.URL, &body)
	if err != nil {
		return nil, fmt.Errorf("httpocr: new request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req, nil
}

// words flattens whichever dialect the server answered in.
func (r ocrResponse) words() []parserails.Word {
	out := make([]parserails.Word, 0, len(r.Results)+len(r.Words))
	for _, res := range r.Results {
		if res.Text == "" {
			continue
		}
		x0, y0, x1, y1, ok := boxOf(res.Bbox, res.Polygon)
		if !ok {
			continue
		}
		out = append(out, parserails.Word{
			Text: res.Text, X0: x0, Y0: y0, X1: x1, Y1: y1,
			Confidence: res.Confidence,
		})
	}
	for _, w := range r.Words {
		if w.Text == "" {
			continue
		}
		out = append(out, parserails.Word{
			Text: w.Text, X0: w.X0, Y0: w.Y0, X1: w.X1, Y1: w.Y1,
			Confidence: w.Confidence,
		})
	}
	return out
}

// boxOf takes the axis-aligned box from `bbox`, or derives it from the
// detection polygon when the server only sent one (rotated text).
func boxOf(bbox []float64, polygon [][]float64) (x0, y0, x1, y1 float64, ok bool) {
	if len(bbox) >= 4 {
		return min(bbox[0], bbox[2]), min(bbox[1], bbox[3]),
			max(bbox[0], bbox[2]), max(bbox[1], bbox[3]), true
	}
	if len(polygon) == 0 {
		return 0, 0, 0, 0, false
	}
	first := true
	for _, pt := range polygon {
		if len(pt) < 2 {
			continue
		}
		if first {
			x0, y0, x1, y1 = pt[0], pt[1], pt[0], pt[1]
			first = false
			continue
		}
		x0, y0 = min(x0, pt[0]), min(y0, pt[1])
		x1, y1 = max(x1, pt[0]), max(y1, pt[1])
	}
	return x0, y0, x1, y1, !first
}

func encodePNG(buf *bytes.Buffer, img image.Image) error {
	if err := png.Encode(buf, img); err != nil {
		return fmt.Errorf("httpocr: encode png: %w", err)
	}
	return nil
}
