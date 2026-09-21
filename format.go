package parserails

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Format identifies a document format. It is what ParseRails routes on: PDFs go
// straight to PDFium, office documents through LibreOffice, images through OCR,
// and container formats get unpacked.
type Format string

// The formats ParseRails recognizes.
const (
	FormatUnknown Format = ""
	FormatPDF     Format = "pdf"

	FormatDOCX Format = "docx"
	FormatXLSX Format = "xlsx"
	FormatPPTX Format = "pptx"
	FormatODT  Format = "odt"
	FormatODS  Format = "ods"
	FormatODP  Format = "odp"
	FormatRTF  Format = "rtf"

	// FormatOLE is the legacy Compound File Binary container behind .doc, .xls,
	// .ppt and .msg. Sniffing bytes alone cannot tell those apart; a filename
	// refines it to one of the four below.
	FormatOLE Format = "ole"
	FormatDOC Format = "doc"
	FormatXLS Format = "xls"
	FormatPPT Format = "ppt"
	FormatMSG Format = "msg"

	FormatPNG  Format = "png"
	FormatJPEG Format = "jpeg"
	FormatGIF  Format = "gif"
	FormatTIFF Format = "tiff"
	FormatBMP  Format = "bmp"
	FormatWEBP Format = "webp"

	FormatZIP  Format = "zip"
	FormatEML  Format = "eml"
	FormatText Format = "txt"
)

// IsImage reports whether the format is a raster image, which ParseRails reads
// through an OCR backend rather than through PDFium.
func (f Format) IsImage() bool {
	switch f {
	case FormatPNG, FormatJPEG, FormatGIF, FormatTIFF, FormatBMP, FormatWEBP:
		return true
	}
	return false
}

// IsOffice reports whether the format is an office document ParseRails converts
// to PDF before parsing.
func (f Format) IsOffice() bool {
	switch f {
	case FormatDOCX, FormatXLSX, FormatPPTX, FormatODT, FormatODS, FormatODP,
		FormatRTF, FormatDOC, FormatXLS, FormatPPT:
		return true
	}
	return false
}

// IsOOXML reports whether the format is an Office Open XML package (the ZIP +
// XML formats ParseRails can also read natively, without LibreOffice).
func (f Format) IsOOXML() bool {
	switch f {
	case FormatDOCX, FormatXLSX, FormatPPTX:
		return true
	}
	return false
}

// Ext returns the conventional file extension for the format, including the
// leading dot, or "" when it has none.
func (f Format) Ext() string {
	switch f {
	case FormatUnknown:
		return ""
	case FormatOLE:
		return ".bin"
	case FormatJPEG:
		return ".jpg"
	default:
		return "." + string(f)
	}
}

func (f Format) String() string {
	if f == FormatUnknown {
		return "unknown"
	}
	return string(f)
}

// byExt maps file extensions to formats.
var byExt = map[string]Format{
	".pdf":  FormatPDF,
	".docx": FormatDOCX, ".docm": FormatDOCX,
	".xlsx": FormatXLSX, ".xlsm": FormatXLSX,
	".pptx": FormatPPTX, ".pptm": FormatPPTX,
	".odt": FormatODT, ".ods": FormatODS, ".odp": FormatODP,
	".rtf": FormatRTF,
	".doc": FormatDOC, ".xls": FormatXLS, ".ppt": FormatPPT, ".msg": FormatMSG,
	".png": FormatPNG, ".jpg": FormatJPEG, ".jpeg": FormatJPEG,
	".gif": FormatGIF, ".tif": FormatTIFF, ".tiff": FormatTIFF,
	".bmp": FormatBMP, ".webp": FormatWEBP,
	".zip": FormatZIP, ".eml": FormatEML,
	".txt": FormatText, ".md": FormatText, ".csv": FormatText,
	".tsv": FormatText, ".json": FormatText, ".log": FormatText,
}

// FormatByName maps a file name or path to a format by its extension alone.
// It returns FormatUnknown for extensions ParseRails does not handle.
func FormatByName(name string) Format {
	return byExt[strings.ToLower(filepath.Ext(name))]
}

// sniffLimit is how far into the data the text and e-mail heuristics look.
const sniffLimit = 4096

// Sniff identifies a format from the leading bytes of a document.
//
// Extensions lie: a `.pdf` that is really a DOCX, an attachment named
// `invoice.dat`, or a byte slice with no name at all are all routine. Magic
// bytes do not, so content wins wherever it is conclusive.
func Sniff(data []byte) Format {
	if len(data) == 0 {
		return FormatUnknown
	}
	switch {
	case hasPDFHeader(data):
		return FormatPDF
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return FormatPNG
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return FormatJPEG
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return FormatGIF
	case bytes.HasPrefix(data, []byte("II*\x00")), bytes.HasPrefix(data, []byte("MM\x00*")):
		return FormatTIFF
	case bytes.HasPrefix(data, []byte("BM")):
		return FormatBMP
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return FormatWEBP
	case bytes.HasPrefix(data, oleMagic):
		return FormatOLE
	case bytes.HasPrefix(data, []byte("{\\rtf")):
		return FormatRTF
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		return sniffZip(data)
	}
	if looksLikeEmail(data) {
		return FormatEML
	}
	if looksLikeText(data) {
		return FormatText
	}
	return FormatUnknown
}

// oleMagic is the Compound File Binary header signature.
var oleMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// hasPDFHeader looks for %PDF near the start. The spec puts it at byte 0, but
// files with leading junk are common and PDFium accepts them.
func hasPDFHeader(data []byte) bool {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return bytes.Contains(head, []byte("%PDF-"))
}

// sniffZip distinguishes the ZIP-based document formats by their package
// layout, falling back to a plain archive.
func sniffZip(data []byte) Format {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return FormatZIP
	}
	for _, f := range zr.File {
		switch {
		case strings.HasPrefix(f.Name, "word/"):
			return FormatDOCX
		case strings.HasPrefix(f.Name, "xl/"):
			return FormatXLSX
		case strings.HasPrefix(f.Name, "ppt/"):
			return FormatPPTX
		case f.Name == "mimetype":
			if mt := readZipEntry(f, 128); mt != "" {
				switch {
				case strings.Contains(mt, "opendocument.text"):
					return FormatODT
				case strings.Contains(mt, "opendocument.spreadsheet"):
					return FormatODS
				case strings.Contains(mt, "opendocument.presentation"):
					return FormatODP
				}
			}
		}
	}
	return FormatZIP
}

func readZipEntry(f *zip.File, limit int) string {
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer func() { _ = rc.Close() }()
	buf := make([]byte, limit)
	n, _ := rc.Read(buf)
	return string(buf[:n])
}

// emailHeaders are the headers that distinguish an RFC 5322 message from any
// other text file that happens to start with a word and a colon.
var emailHeaders = [][]byte{
	[]byte("From:"), []byte("To:"), []byte("Subject:"),
	[]byte("Message-ID:"), []byte("Date:"), []byte("Received:"),
}

func looksLikeEmail(data []byte) bool {
	head := data
	if len(head) > sniffLimit {
		head = head[:sniffLimit]
	}
	if bytes.HasPrefix(head, []byte("From ")) { // mbox "From " separator line
		return true
	}
	if !bytes.Contains(head, []byte("MIME-Version:")) && !bytes.Contains(head, []byte("Content-Type:")) {
		// A message without either is still a message, but only if it opens
		// with real headers; require two of them to avoid grabbing prose.
		found := 0
		for _, h := range emailHeaders {
			if bytes.Contains(head, h) {
				found++
			}
		}
		return found >= 2 && startsWithHeaderLine(head)
	}
	return startsWithHeaderLine(head)
}

// startsWithHeaderLine reports whether the data opens with "Header-Name:".
func startsWithHeaderLine(head []byte) bool {
	for i, c := range head {
		switch {
		case c == ':':
			return i > 0
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '-', c >= '0' && c <= '9':
			continue
		default:
			return false
		}
	}
	return false
}

func looksLikeText(data []byte) bool {
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	// A truncated multi-byte rune at the sample edge is fine; anything else
	// invalid means this is not text.
	for len(sample) > 0 {
		r, size := utf8.DecodeRune(sample)
		if r == utf8.RuneError && size == 1 {
			return len(sample) < utf8.UTFMax
		}
		sample = sample[size:]
	}
	return true
}

// Detect identifies a document's format from its content, using the name only
// where the bytes are ambiguous (legacy OLE documents) or inconclusive.
func Detect(name string, data []byte) Format {
	sniffed := Sniff(data)
	byName := FormatByName(name)

	switch {
	case sniffed == FormatOLE:
		// CFB carries .doc, .xls, .ppt and .msg alike; only the name tells them
		// apart, and only when it is one of those.
		switch byName {
		case FormatDOC, FormatXLS, FormatPPT, FormatMSG:
			return byName
		}
		return FormatOLE
	case sniffed == FormatText && byName != FormatUnknown && byName != FormatText:
		// Text-like but named as something structured (e.g. an .rtf or .eml
		// that our heuristics read as prose): trust the name.
		return byName
	case sniffed != FormatUnknown:
		return sniffed
	default:
		return byName
	}
}

// IsOfficeFormat reports whether path has an extension ParseRails converts to
// PDF via LibreOffice before parsing.
func IsOfficeFormat(path string) bool { return FormatByName(path).IsOffice() }
