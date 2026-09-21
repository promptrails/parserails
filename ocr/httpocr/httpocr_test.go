package httpocr

import (
	"context"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecognizeOCRAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not a multipart request: %v", err)
			return
		}
		if got := r.FormValue("language"); got != "tur" {
			t.Errorf("language = %q, want tur", got)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("no file field: %v", err)
			return
		}
		defer func() { _ = file.Close() }()
		if header.Filename == "" {
			t.Error("file part has no filename")
		}
		if n, _ := io.Copy(io.Discard, file); n == 0 {
			t.Error("file part is empty")
		}
		if r.Header.Get("X-Token") != "secret" {
			t.Error("custom header not forwarded")
		}
		_, _ = w.Write([]byte(`{"results":[
			{"text":"Hello","bbox":[10,20,50,32],"confidence":0.95},
			{"text":"Rotated","polygon":[[60,20],[90,22],[90,34],[60,32]],"confidence":0.7},
			{"text":"","bbox":[0,0,0,0]}
		]}`))
	}))
	defer srv.Close()

	b := New(Config{URL: srv.URL, Language: "tur", Header: http.Header{"X-Token": {"secret"}}})
	words, err := b.Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(words) != 2 { // the blank result is dropped
		t.Fatalf("got %d words, want 2: %+v", len(words), words)
	}
	if w := words[0]; w.Text != "Hello" || w.X0 != 10 || w.Y0 != 20 || w.X1 != 50 || w.Y1 != 32 {
		t.Errorf("word = %+v", w)
	}
	if got := words[0].Confidence; got != 0.95 {
		t.Errorf("confidence = %v, want 0.95", got)
	}
	// The polygon-only result is reduced to its axis-aligned bounds.
	if w := words[1]; w.X0 != 60 || w.Y0 != 20 || w.X1 != 90 || w.Y1 != 34 {
		t.Errorf("polygon word = %+v, want [60 20 90 34]", w)
	}
}

func TestRecognizeWordsProtocol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "image/png" {
			t.Errorf("Content-Type = %q, want image/png", ct)
		}
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"words":[{"text":"Hello","x0":10,"y0":20,"x1":50,"y1":32,"confidence":0.8}]}`))
	}))
	defer srv.Close()

	b := New(Config{URL: srv.URL, Protocol: ProtocolWords})
	words, err := b.Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(words) != 1 || words[0].Text != "Hello" || words[0].Confidence != 0.8 {
		t.Fatalf("words = %+v", words)
	}
}

func TestRecognizeAcceptsEitherDialect(t *testing.T) {
	// A server configured for one protocol may answer in the other shape; the
	// decoder takes whichever arrived.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"words": []map[string]any{{"text": "legacy", "x0": 1, "y0": 2, "x1": 3, "y1": 4}},
		})
	}))
	defer srv.Close()

	b := New(Config{URL: srv.URL}) // OCR API protocol
	words, err := b.Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 4, 4)))
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(words) != 1 || words[0].Text != "legacy" {
		t.Fatalf("words = %+v", words)
	}
}

func TestRecognizeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := New(Config{URL: srv.URL})
	if _, err := b.Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 4, 4))); err == nil {
		t.Fatal("expected an error on 500")
	}
}

func TestURLRequired(t *testing.T) {
	b := New(Config{})
	if _, err := b.Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 4, 4))); err == nil {
		t.Fatal("expected an error when URL is empty")
	}
}
