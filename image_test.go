package parserails

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestParseImageRunsOCRAndFlipsCoordinates(t *testing.T) {
	ocr := &fakeOCR{words: []Word{
		// pixel space, top-left origin: a word 10px from the top of a 100px image.
		{Text: "RECEIPT", X0: 5, Y0: 10, X1: 55, Y1: 22, Confidence: 0.88},
	}}
	p := newTestParser(t, WithOCR(ocr))

	doc, err := p.ParseImage(context.Background(), samplePNG(200, 100))
	if err != nil {
		t.Fatalf("ParseImage: %v", err)
	}
	if len(doc.Pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(doc.Pages))
	}
	page := doc.Pages[0]
	if page.Width != 200 || page.Height != 100 {
		t.Errorf("page size = %vx%v, want the image's 200x100", page.Width, page.Height)
	}
	w := page.Words[0]
	if w.Text != "RECEIPT" || w.Confidence != 0.88 {
		t.Fatalf("word = %+v", w)
	}
	// Top-left pixel Y=10..22 becomes bottom-left Y=78..90.
	if w.Y0 != 78 || w.Y1 != 90 {
		t.Errorf("y = %v..%v, want 78..90 (origin flipped)", w.Y0, w.Y1)
	}
	if w.X0 != 5 || w.X1 != 55 {
		t.Errorf("x = %v..%v, want 5..55 (unchanged)", w.X0, w.X1)
	}
}

func TestParseImageNeedsAnOCRBackend(t *testing.T) {
	p := newTestParser(t)
	_, err := p.ParseImage(context.Background(), samplePNG(8, 8))
	if err == nil || !strings.Contains(err.Error(), "OCR backend") {
		t.Fatalf("err = %v, want it to ask for an OCR backend", err)
	}
}

func TestParseDataRoutesImagesToOCR(t *testing.T) {
	ocr := &fakeOCR{words: []Word{{Text: "SCANNED", X0: 1, Y0: 1, X1: 20, Y1: 10}}}
	p := newTestParser(t, WithOCR(ocr))

	doc, err := p.ParseData(context.Background(), samplePNG(64, 32), ReadOptions{Name: "scan.png"})
	if err != nil {
		t.Fatalf("ParseData: %v", err)
	}
	if got := doc.Text(); got != "SCANNED" {
		t.Fatalf("text = %q, want %q", got, "SCANNED")
	}

	text, err := p.ExtractTextData(context.Background(), samplePNG(64, 32), ReadOptions{})
	if err != nil {
		t.Fatalf("ExtractTextData: %v", err)
	}
	if text != "SCANNED" {
		t.Fatalf("text = %q, want %q", text, "SCANNED")
	}
}

func TestInspectImageIsAlwaysAScan(t *testing.T) {
	p := newTestParser(t)
	c, err := p.Inspect(context.Background(), samplePNG(64, 32), ReadOptions{})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !c.NeedsOCR() || !contains(c.Pages[0].Reasons, ReasonScanned) {
		t.Fatalf("complexity = %+v, want a scanned verdict", c.Pages)
	}
}

func TestParseImageRejectsOversizedHeaders(t *testing.T) {
	ocr := &fakeOCR{}
	p := newTestParser(t, WithOCR(ocr))

	// A valid PNG header declaring 60000x60000 with no pixel data behind it:
	// decoding it would reserve gigabytes before failing.
	_, err := p.ParseImage(context.Background(), pngHeader(60000, 60000))
	if err == nil || !strings.Contains(err.Error(), "megapixel limit") {
		t.Fatalf("err = %v, want a size-limit refusal", err)
	}
	if len(ocr.crops) != 0 {
		t.Error("OCR should not have run")
	}
}

// pngHeader builds a PNG signature plus a valid IHDR for the given size, and
// nothing else.
func pngHeader(width, height uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	_ = binary.Write(&ihdr, binary.BigEndian, width)
	_ = binary.Write(&ihdr, binary.BigEndian, height)
	ihdr.Write([]byte{8, 2, 0, 0, 0}) // 8-bit truecolour

	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&out, binary.BigEndian, uint32(ihdr.Len()-4)) // length excludes the type
	out.Write(ihdr.Bytes())
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}

func samplePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.White)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
