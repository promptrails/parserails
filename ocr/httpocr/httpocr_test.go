package httpocr

import (
	"context"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecognize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "image/png" {
			t.Errorf("Content-Type = %q, want image/png", ct)
		}
		if r.Header.Get("X-Token") != "secret" {
			t.Errorf("custom header not forwarded")
		}
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"words":[
			{"text":"Hello","x0":10,"y0":20,"x1":50,"y1":32},
			{"text":"","x0":0,"y0":0,"x1":0,"y1":0}
		]}`))
	}))
	defer srv.Close()

	b := New(Config{URL: srv.URL, Header: http.Header{"X-Token": {"secret"}}})
	words, err := b.Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(words) != 1 { // the blank word is dropped
		t.Fatalf("got %d words, want 1: %+v", len(words), words)
	}
	if w := words[0]; w.Text != "Hello" || w.X0 != 10 || w.Y0 != 20 || w.X1 != 50 || w.Y1 != 32 {
		t.Errorf("word = %+v", w)
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
