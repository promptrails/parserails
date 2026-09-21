package parserails

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSniff(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want Format
	}{
		{"pdf", minimalPDF("hi"), FormatPDF},
		{"png", []byte("\x89PNG\r\n\x1a\n....."), FormatPNG},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}, FormatJPEG},
		{"gif", []byte("GIF89a...."), FormatGIF},
		{"tiff", []byte("II*\x00...."), FormatTIFF},
		{"bmp", []byte("BM\x00\x00\x00"), FormatBMP},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), FormatWEBP},
		{"ole", append(append([]byte{}, oleMagic...), 0x00, 0x01), FormatOLE},
		{"rtf", []byte(`{\rtf1\ansi hello}`), FormatRTF},
		{"docx", ooxmlZip("word/document.xml"), FormatDOCX},
		{"xlsx", ooxmlZip("xl/workbook.xml"), FormatXLSX},
		{"pptx", ooxmlZip("ppt/presentation.xml"), FormatPPTX},
		{"zip", ooxmlZip("notes/readme.md"), FormatZIP},
		{"eml", []byte("From: a@b.c\r\nTo: d@e.f\r\nSubject: hi\r\n\r\nbody"), FormatEML},
		{"text", []byte("just some prose, nothing magic about it"), FormatText},
		{"binary", []byte{0x01, 0x00, 0x02, 0xFE, 0xFF}, FormatUnknown},
		{"empty", nil, FormatUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Sniff(tc.data); got != tc.want {
				t.Fatalf("Sniff(%s) = %s, want %s", tc.name, got, tc.want)
			}
		})
	}
}

func TestDetectPrefersContentOverExtension(t *testing.T) {
	// A DOCX that someone named .pdf — the bytes decide.
	if got := Detect("invoice.pdf", ooxmlZip("word/document.xml")); got != FormatDOCX {
		t.Errorf("mislabeled docx detected as %s, want docx", got)
	}
	// CFB is ambiguous; the name refines it.
	ole := append(append([]byte{}, oleMagic...), 0x00)
	if got := Detect("letter.doc", ole); got != FormatDOC {
		t.Errorf("Detect(letter.doc) = %s, want doc", got)
	}
	if got := Detect("mail.msg", ole); got != FormatMSG {
		t.Errorf("Detect(mail.msg) = %s, want msg", got)
	}
	if got := Detect("", ole); got != FormatOLE {
		t.Errorf("Detect(unnamed ole) = %s, want ole", got)
	}
	// Unknown bytes fall back to the name.
	if got := Detect("notes.txt", []byte{0x00, 0x01}); got != FormatText {
		t.Errorf("Detect(binary named .txt) = %s, want txt", got)
	}
}

func TestFormatPredicates(t *testing.T) {
	if !FormatDOCX.IsOffice() || !FormatDOCX.IsOOXML() {
		t.Error("docx should be an office and an OOXML format")
	}
	if FormatMSG.IsOffice() {
		t.Error("msg is an e-mail container, not an office document")
	}
	if !FormatPNG.IsImage() || FormatPDF.IsImage() {
		t.Error("image predicate is wrong")
	}
	if got := FormatJPEG.Ext(); got != ".jpg" {
		t.Errorf("jpeg ext = %q, want .jpg", got)
	}
}

func TestParseRejectsNonPDFData(t *testing.T) {
	p := newTestParser(t)
	_, err := p.Parse(context.Background(), ooxmlZip("word/document.xml"))
	if err == nil || !strings.Contains(err.Error(), "docx") {
		t.Fatalf("Parse(docx) error = %v, want it to name the format", err)
	}
}

func TestParseDataAndParseFileRejectUnparsableInput(t *testing.T) {
	p := newTestParser(t)
	if _, err := p.ParseData(context.Background(), []byte("hello"), ReadOptions{Name: "a.txt"}); err == nil {
		t.Fatal("ParseData(text) should fail")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ParseFile(context.Background(), path); err == nil {
		t.Fatal("ParseFile(text) should fail")
	}
}

func TestParseFileDetectsPDFByContent(t *testing.T) {
	p := newTestParser(t)
	dir := t.TempDir()
	// A PDF with a misleading extension still parses.
	path := filepath.Join(dir, "report.dat")
	if err := os.WriteFile(path, minimalPDF("Hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := p.ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got := doc.Text(); got != "Hello" {
		t.Fatalf("text = %q, want %q", got, "Hello")
	}
}

// ooxmlZip builds a ZIP archive containing a single named entry.
func ooxmlZip(entry string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(entry)
	if err != nil {
		panic(err)
	}
	if _, err := w.Write([]byte("<xml/>")); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
