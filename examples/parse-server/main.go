// parse-server is a small HTTP service built on ParseRails. POST a document and
// get back its spatial text as JSON. It wires in Tesseract OCR so scanned PDFs
// still produce text, and accepts office documents (converted via LibreOffice).
//
//	go run .                         # listens on :8080
//	curl -F file=@doc.pdf localhost:8080/parse
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/promptrails/parserails"
	"github.com/promptrails/parserails/ocr/tesseract"
)

func main() {
	addr := envOr("ADDR", ":8080")

	p, err := parserails.New(
		parserails.WithOCR(tesseract.New(tesseract.Config{Lang: envOr("OCR_LANG", "eng")})),
		parserails.WithPoolSize(2, 4, 8),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	srv := &server{p: p}
	http.HandleFunc("/parse", srv.handleParse)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	log.Printf("parse-server (%s backend) listening on %s", parserails.Backend, addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

type server struct{ p *parserails.Parser }

// handleParse accepts a multipart "file" upload (or a raw body) and returns the
// parsed document as JSON.
func (s *server) handleParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST a document to /parse", http.StatusMethodNotAllowed)
		return
	}

	name, data, err := readUpload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// ParseFile handles both PDFs and office docs (the latter via LibreOffice),
	// so persist the upload to a temp file keyed by its original extension.
	tmp, err := os.CreateTemp("", "upload-*"+filepath.Ext(name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = tmp.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	doc, err := s.p.ParseFile(ctx, tmp.Name())
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
}

func readUpload(r *http.Request) (name string, data []byte, err error) {
	if file, hdr, ferr := r.FormFile("file"); ferr == nil {
		defer func() { _ = file.Close() }()
		data, err = io.ReadAll(file)
		return hdr.Filename, data, err
	}
	// Fall back to a raw body; assume PDF.
	data, err = io.ReadAll(r.Body)
	return "upload.pdf", data, err
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
