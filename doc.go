// Package parserails reads documents: PDFs, office files, e-mail, archives,
// and scans, without cgo and without a cloud service.
//
// PDF text comes from Google's PDFium, the engine behind Chrome's PDF viewer,
// compiled to WebAssembly and run under wazero. Nothing here links C: a
// ParseRails binary is static, cross-compiles cleanly, and needs no libpdfium
// in your image. Build with -tags parserails_cgo to link the native library
// instead, for throughput on a host you control.
//
// ParseRails is the Go counterpart to run-llama/liteparse, and uses the same
// engine.
//
// # Getting started
//
// Create one [Parser] per process — it owns a pooled runtime and is safe for
// concurrent use — and give it bytes or a path:
//
//	p, err := parserails.New()
//	if err != nil {
//		return err
//	}
//	defer p.Close()
//
//	doc, err := p.ParseFile(ctx, "invoice.pdf")
//	if err != nil {
//		return err
//	}
//	for _, w := range doc.Words() {
//		fmt.Printf("p%d %q [%.1f %.1f %.1f %.1f]\n", w.Page, w.Text, w.X0, w.Y0, w.X1, w.Y1)
//	}
//
// # Which entry point
//
// Every one of these takes a context and returns on cancellation:
//
//   - [Parser.Parse] parses PDF bytes with the parser's defaults.
//   - [Parser.ParseData] parses anything, detecting the format from the bytes,
//     and takes [ReadOptions] for a password or a page range.
//   - [Parser.ParseFile] does the same for a path, converting office
//     documents through LibreOffice.
//   - [Parser.ExtractText] and [Parser.ExtractTextData] skip the boxes and use
//     PDFium's whole-page text API, which is several times cheaper.
//   - [Parser.ParseImage] reads a scan or a photo through the OCR backend.
//   - [Parser.Inspect] reports which pages need OCR before you pay for any.
//   - [Parser.Extract] walks a file and everything inside it.
//   - [Parser.RenderPage] rasterizes one page.
//
// # What comes back
//
// A [Document] is pages of [Word]s, each with its text, page, bounding box in
// PDF points, font size and — for recognized text — a confidence. On top of
// that geometry sit [Line]s, reconstructed by grouping words that share a
// band, and [Block]s: headings, paragraphs, list items, tables and figures,
// classified from the layout. [Document.Markdown] renders those blocks, which
// is usually what an LLM or a RAG index wants.
//
// Coordinates are PDF user space everywhere: origin bottom-left, units of
// 1/72 inch.
//
// # OCR
//
// Nothing is recognized unless you provide a backend, through [WithOCR]. A
// backend implements [OCR]: an image in, words with pixel-space boxes out,
// which ParseRails maps back into the page. Two ship with the library —
// ocr/tesseract shells out to the binary, ocr/httpocr talks to a server over
// the LiteParse OCR API — and a page with no extractable text falls back to
// whichever is configured. [WithImageOCR] additionally reads the figures on
// pages that do have text.
//
// # Files inside files
//
// A real corpus is an invoice attached to an e-mail inside an archive.
// [Parser.Extract] walks that: PDF attachments, ZIP entries, e-mail and
// Outlook attachments, the objects embedded in office documents, and whatever
// is inside those. The walk is bounded by depth, file count and unpacked
// bytes, and a file it cannot read becomes a [Node] carrying the reason
// rather than failing the walk. [WithContainer] teaches it a format it does
// not know.
//
// # Office documents without LibreOffice
//
// [ReadOfficeDocument] reads a DOCX, XLSX or PPTX straight out of the
// package. There are no page coordinates — nothing has been laid out — but
// the document's own structure is better than any inference from geometry:
// heading levels as declared, table cells as authored, spreadsheet rows in
// their real columns. [WithNativeOffice] prefers it wherever text rather than
// geometry is being asked for.
package parserails
