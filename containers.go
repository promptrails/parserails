package parserails

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"

	"github.com/klippa-app/go-pdfium/requests"
)

// maxChildBytes caps a single embedded file. The walk has its own total
// budget; this stops one entry from spending all of it.
const maxChildBytes = 128 << 20

// zipContainer yields every file in a ZIP archive.
type zipContainer struct{}

func (zipContainer) Children(_ context.Context, data []byte, req ChildRequest) ([]Child, error) {
	return zipEntries(data, req, func(string) bool { return true })
}

// ooxmlContainer yields the files embedded in an Office Open XML package.
//
// The package is a ZIP full of the document's own XML, which is not what a
// caller means by "what is inside this file": only /embeddings/ holds the
// attached objects — a spreadsheet pasted into a report, an attached PDF.
type ooxmlContainer struct{}

func (ooxmlContainer) Children(_ context.Context, data []byte, req ChildRequest) ([]Child, error) {
	return zipEntries(data, req, func(name string) bool {
		return strings.Contains(strings.ToLower(name), "/embeddings/")
	})
}

// zipEntries unpacks the entries a filter keeps, within the walk's remaining
// budget.
//
// The budget is spent here, as each entry is decompressed, rather than counted
// afterwards: an archive of a thousand entries that each expand to 64 MiB is
// 64 GiB of allocation before a caller that only checks totals ever sees the
// first one.
func zipEntries(data []byte, req ChildRequest, keep func(string) bool) ([]Child, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("parserails: read zip: %w", err)
	}

	remaining := req.MaxBytes
	var out []Child
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !keep(f.Name) {
			continue
		}
		if req.MaxFiles > 0 && len(out) >= req.MaxFiles {
			out = append(out, Child{Name: f.Name,
				Err: fmt.Errorf("parserails: extraction stopped: more files than the limit allows")})
			break
		}

		limit := int64(maxChildBytes)
		if remaining > 0 && remaining < limit {
			limit = remaining
		}
		body, err := readZipFile(f, limit)
		switch {
		case err != nil:
			// A broken entry is not a broken archive.
			out = append(out, Child{Name: f.Name, Err: fmt.Errorf("parserails: read zip entry: %w", err)})
			continue
		case int64(len(body)) > limit:
			out = append(out, Child{Name: f.Name,
				Err: fmt.Errorf("parserails: entry is larger than the remaining %d byte budget", limit)})
			continue
		}
		if remaining > 0 {
			remaining -= int64(len(body))
		}
		out = append(out, Child{Name: f.Name, Data: body})
	}
	return out, nil
}

// readZipFile decompresses one entry, reading one byte past the limit so the
// caller can tell "exactly at the limit" from "over it". The declared
// uncompressed size is not trusted: it is written by whoever built the archive.
func readZipFile(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, limit+1))
}

// pdfContainer yields a PDF's embedded file attachments.
type pdfContainer struct{ parser *Parser }

func (c pdfContainer) Children(_ context.Context, data []byte, req ChildRequest) ([]Child, error) {
	inst, err := c.parser.pool.GetInstance(c.parser.acquireTimeout())
	if err != nil {
		return nil, fmt.Errorf("parserails: acquire instance: %w", err)
	}
	defer func() { _ = inst.Close() }()

	// The password the caller opened this read with, not just the parser's
	// default: otherwise a document that parsed fine reports itself encrypted
	// the moment its attachments are enumerated.
	password := req.Password
	if password == "" {
		password = c.parser.password
	}
	doc, err := openDocument(inst, data, password)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	}()

	count, err := inst.FPDFDoc_GetAttachmentCount(&requests.FPDFDoc_GetAttachmentCount{
		Document: doc.Document,
	})
	if err != nil {
		if unsupported(err) {
			return nil, nil // this backend cannot read attachments
		}
		return nil, fmt.Errorf("parserails: attachment count: %w", err)
	}

	var out []Child
	for i := 0; i < count.AttachmentCount; i++ {
		att, err := inst.FPDFDoc_GetAttachment(&requests.FPDFDoc_GetAttachment{
			Document: doc.Document, Index: i,
		})
		if err != nil {
			continue
		}
		name := fmt.Sprintf("attachment-%d", i+1)
		if got, err := inst.FPDFAttachment_GetName(&requests.FPDFAttachment_GetName{
			Attachment: att.Attachment,
		}); err == nil && got.Name != "" {
			name = got.Name
		}
		file, err := inst.FPDFAttachment_GetFile(&requests.FPDFAttachment_GetFile{
			Attachment: att.Attachment,
		})
		if err != nil || len(file.Contents) == 0 {
			continue
		}
		out = append(out, Child{Name: name, Data: file.Contents})
	}
	return out, nil
}

// emlContainer yields an e-mail's attachments; the body is read separately by
// emlText.
type emlContainer struct{}

func (emlContainer) Children(_ context.Context, data []byte, _ ChildRequest) ([]Child, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parserails: read e-mail: %w", err)
	}
	_, attachments, err := walkMailPart(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Body)
	if err != nil {
		return nil, err
	}
	return attachments, nil
}

// oleContainer yields the payloads embedded in a Compound File: the classic
// Ole10Native package streams, plus any stream whose bytes are a document in
// their own right.
type oleContainer struct{}

func (oleContainer) Children(_ context.Context, data []byte, _ ChildRequest) ([]Child, error) {
	f, err := openCFB(data)
	if err != nil {
		return nil, err
	}

	var out []Child
	for _, s := range f.streams() {
		body := f.readStream(s.Entry)
		if len(body) == 0 {
			continue
		}
		name := strings.TrimPrefix(s.Path, "\x01")
		if strings.Contains(s.Path, "Ole10Native") {
			if child, ok := parseOle10Native(body); ok {
				out = append(out, child)
				continue
			}
		}
		// Streams of the host document itself (WordDocument, Workbook, the
		// property sets) are not embedded files; only take what stands alone.
		if format := Sniff(body); format == FormatPDF || format.IsOOXML() ||
			format.IsImage() || format == FormatZIP {
			out = append(out, Child{Name: name + format.Ext(), Data: body})
		}
	}
	return out, nil
}

// parseOle10Native unwraps the "package" stream Windows writes when a file is
// dropped into a document. The header is a string of NUL-terminated paths
// followed by the payload's own length, and implementations vary enough that
// a failed parse falls back to finding the payload by its signature.
func parseOle10Native(data []byte) (Child, bool) {
	const minHeader = 10
	if len(data) < minHeader {
		return Child{}, false
	}
	r := &byteReader{data: data, off: 6} // total size (4) + flags (2)
	label, ok1 := r.cstring()
	_, ok2 := r.cstring() // original full path
	r.skip(8)             // two reserved longs
	_, ok3 := r.cstring() // temporary path
	size, ok4 := r.uint32()
	if ok1 && ok2 && ok3 && ok4 && size > 0 && int(size) <= r.left() {
		name := sanitizeChildName(label)
		payload := r.take(int(size))
		if name == "" {
			name = "payload" + Sniff(payload).Ext()
		}
		return Child{Name: name, Data: payload}, true
	}

	// Fallback: the payload starts at the earliest signature in the stream —
	// earliest in the file, not first in this list, or a ZIP whose contents
	// mention "%PDF-" would be cut at the mention.
	start := -1
	for _, magic := range [][]byte{[]byte("%PDF-"), []byte("PK\x03\x04"), {0xFF, 0xD8, 0xFF}, []byte("\x89PNG")} {
		if i := bytes.Index(data, magic); i >= 0 && (start < 0 || i < start) {
			start = i
		}
	}
	if start < 0 {
		return Child{}, false
	}
	payload := data[start:]
	return Child{Name: "payload" + Sniff(payload).Ext(), Data: payload}, true
}

func sanitizeChildName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// byteReader reads the little-endian, NUL-terminated soup that OLE headers
// are made of.
type byteReader struct {
	data []byte
	off  int
}

func (r *byteReader) left() int { return len(r.data) - r.off }

func (r *byteReader) skip(n int) { r.off = min(r.off+n, len(r.data)) }

func (r *byteReader) cstring() (string, bool) {
	i := bytes.IndexByte(r.data[r.off:], 0)
	if i < 0 {
		return "", false
	}
	s := string(r.data[r.off : r.off+i])
	r.off += i + 1
	return s, true
}

func (r *byteReader) uint32() (uint32, bool) {
	if r.left() < 4 {
		return 0, false
	}
	v := binary.LittleEndian.Uint32(r.data[r.off : r.off+4])
	r.off += 4
	return v, true
}

func (r *byteReader) take(n int) []byte {
	n = min(n, r.left())
	out := r.data[r.off : r.off+n]
	r.off += n
	return out
}

// mimeFilename pulls a file name out of a MIME part's headers.
func mimeFilename(disposition, contentType string, index int) string {
	for _, header := range []string{disposition, contentType} {
		if header == "" {
			continue
		}
		if _, params, err := mime.ParseMediaType(header); err == nil {
			for _, key := range []string{"filename", "name"} {
				if v := strings.TrimSpace(params[key]); v != "" {
					return sanitizeChildName(v)
				}
			}
		}
	}
	return fmt.Sprintf("attachment-%d", index)
}
