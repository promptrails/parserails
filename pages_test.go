package parserails

import (
	"context"
	"crypto/md5" // #nosec G501 -- the PDF standard security handler is defined in terms of MD5 and RC4.
	"crypto/rc4" // #nosec G503
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParsePageSpec(t *testing.T) {
	cases := []struct {
		spec  string
		count int
		want  []int
	}{
		{"1", 5, []int{0}},
		{"1-3", 5, []int{0, 1, 2}},
		{"1-3,5", 5, []int{0, 1, 2, 4}},
		{"3,1", 5, []int{2, 0}},            // order is as written
		{"1-3,2", 5, []int{0, 1, 2}},       // duplicates collapse
		{"4-10", 5, []int{3, 4}},           // past the end is clipped
		{" 2 - 3 , 5 ", 5, []int{1, 2, 4}}, // whitespace tolerated
		{"9", 5, nil},
	}
	for _, tc := range cases {
		got, err := parsePageSpec(tc.spec, tc.count)
		if err != nil {
			t.Errorf("parsePageSpec(%q): %v", tc.spec, err)
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("parsePageSpec(%q) = %v, want %v", tc.spec, got, tc.want)
		}
	}
	// A range far past the document is clipped, not walked: these return
	// immediately rather than iterating billions of times.
	for _, spec := range []string{"1-1000000000", "1-9223372036854775807", "2-9223372036854775807"} {
		done := make(chan []int, 1)
		go func() {
			got, err := parsePageSpec(spec, 3)
			if err != nil {
				t.Errorf("parsePageSpec(%q): %v", spec, err)
			}
			done <- got
		}()
		select {
		case got := <-done:
			if len(got) > 3 {
				t.Errorf("parsePageSpec(%q) returned %d pages for a 3-page document", spec, len(got))
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("parsePageSpec(%q) did not terminate", spec)
		}
	}

	for _, bad := range []string{"0", "abc", "3-1", "-2"} {
		if _, err := parsePageSpec(bad, 5); err == nil {
			t.Errorf("parsePageSpec(%q) should fail", bad)
		}
	}
}

func TestSelectPagesAppliesMaxPages(t *testing.T) {
	got, err := selectPages(10, ReadOptions{MaxPages: 3})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint([]int{0, 1, 2}) {
		t.Fatalf("got %v, want the first three pages", got)
	}
	got, err = selectPages(10, ReadOptions{Pages: "5-8", MaxPages: 2})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint([]int{4, 5}) {
		t.Fatalf("got %v, want the first two selected pages", got)
	}
}

func TestParseDataSelectsPages(t *testing.T) {
	p := newTestParser(t)
	pdf := pdfWithPages([][]textRun{
		{{Text: "one", X: 72, Y: 700}},
		{{Text: "two", X: 72, Y: 700}},
		{{Text: "three", X: 72, Y: 700}},
	})

	doc, err := p.ParseData(context.Background(), pdf, ReadOptions{Pages: "2-3"})
	if err != nil {
		t.Fatalf("ParseData: %v", err)
	}
	if got := doc.Text(); got != "two\fthree" {
		t.Fatalf("text = %q, want %q", got, "two\fthree")
	}
	// Page indexes stay absolute so callers know what they got.
	if doc.Pages[0].Index != 1 {
		t.Fatalf("first selected page index = %d, want 1", doc.Pages[0].Index)
	}

	text, err := p.ExtractTextData(context.Background(), pdf, ReadOptions{MaxPages: 1})
	if err != nil {
		t.Fatalf("ExtractTextData: %v", err)
	}
	if !strings.Contains(text, "one") || strings.Contains(text, "two") {
		t.Fatalf("text = %q, want only the first page", text)
	}
}

func TestParseEncryptedDocument(t *testing.T) {
	p := newTestParser(t)
	pdf := encryptedPDF("Secret Report", "hunter2")

	_, err := p.Parse(context.Background(), pdf)
	if err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("Parse without a password: %v, want an 'encrypted' error", err)
	}

	_, err = p.ParseData(context.Background(), pdf, ReadOptions{Password: "wrong"})
	if err == nil || !strings.Contains(err.Error(), "wrong password") {
		t.Fatalf("Parse with a bad password: %v, want a 'wrong password' error", err)
	}

	doc, err := p.ParseData(context.Background(), pdf, ReadOptions{Password: "hunter2"})
	if err != nil {
		t.Fatalf("ParseData with the password: %v", err)
	}
	if got := doc.Text(); got != "Secret Report" {
		t.Fatalf("text = %q, want %q", got, "Secret Report")
	}

	// The same password can be a parser-wide default.
	pd := newTestParser(t, WithPassword("hunter2"))
	if _, err := pd.Parse(context.Background(), pdf); err != nil {
		t.Fatalf("Parse with WithPassword: %v", err)
	}
}

// pdfPad is the 32-byte padding string from the PDF standard security handler.
var pdfPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56,
	0xFF, 0xFA, 0x01, 0x08, 0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
	0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

// encryptedPDF builds a single-page PDF protected with the 40-bit RC4 standard
// security handler (V1/R2) — the classic "password-protected PDF" every reader
// supports. Owner and user password are the same.
func encryptedPDF(text, password string) []byte {
	const fileID = "0123456789abcdef"
	perms := int32(-1)

	padded := padPassword(password)
	ownerKey := md5.Sum(padded) // #nosec G401
	owner := rc4Bytes(ownerKey[:5], padded)

	var permBytes [4]byte
	binary.LittleEndian.PutUint32(permBytes[:], uint32(perms))
	h := md5.New() // #nosec G401
	h.Write(padded)
	h.Write(owner)
	h.Write(permBytes[:])
	h.Write([]byte(fileID))
	fileKey := h.Sum(nil)[:5]
	user := rc4Bytes(fileKey, pdfPad)

	content := fmt.Sprintf("BT /F1 24 Tf 100 700 Td (%s) Tj ET\n", escapePDFString(text))
	encrypted := rc4Bytes(objectKey(fileKey, 4, 0), []byte(content))

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %g %g] /Contents 4 0 R "+
			"/Resources << /Font << /F1 5 0 R >> >> >>", testPageWidth, testPageHeight),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(encrypted), encrypted),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Filter /Standard /V 1 /R 2 /O <%x> /U <%x> /P %d >>", owner, user, perms),
	}

	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, body := range objects {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", len(objects)+1)
	b.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Encrypt %d 0 R /ID [<%x> <%x>] >>\n"+
		"startxref\n%d\n%%%%EOF",
		len(objects)+1, len(objects), fileID, fileID, xref)
	return []byte(b.String())
}

func padPassword(password string) []byte {
	out := make([]byte, 0, 32)
	if len(password) > 32 {
		password = password[:32]
	}
	out = append(out, password...)
	return append(out, pdfPad[:32-len(password)]...)
}

// objectKey derives the per-object RC4 key (PDF 1.7, algorithm 1).
func objectKey(fileKey []byte, num, gen int) []byte {
	h := md5.New() // #nosec G401
	h.Write(fileKey)
	h.Write([]byte{byte(num), byte(num >> 8), byte(num >> 16), byte(gen), byte(gen >> 8)})
	sum := h.Sum(nil)
	n := len(fileKey) + 5
	if n > 16 {
		n = 16
	}
	return sum[:n]
}

func rc4Bytes(key, data []byte) []byte {
	c, err := rc4.NewCipher(key) // #nosec G401
	if err != nil {
		panic(err)
	}
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out
}
