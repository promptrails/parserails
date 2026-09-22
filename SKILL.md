---
name: parserails
description: Parse PDFs, office documents, e-mails, archives and scans locally with the parserails CLI — text, Markdown, per-word bounding boxes, page screenshots, OCR routing, and recursive extraction of files inside files. Use when a task involves reading documents from disk, converting them for RAG or LLM input, or finding out whether a document needs OCR.
---

# ParseRails

A local, cgo-free document parser. No cloud, no API key, no model. Install:

```bash
go install github.com/promptrails/parserails/cmd/parserails@latest
```

PDFs need nothing else. Office formats need `libreoffice` on PATH unless you
pass `--native-office` (DOCX/XLSX/PPTX only). OCR needs `tesseract`, or a
server with `--ocr http --ocr-url …`.

## Choosing a command

| The task | The command |
|----------|-------------|
| Read a document's text | `parserails parse doc.pdf` |
| Feed a document to an LLM or RAG index | `parserails parse --format markdown doc.pdf` |
| Get word positions for citations or highlights | `parserails parse --format json doc.pdf` |
| Convert a whole directory | `parserails batch ./in ./out --format markdown` |
| See what is inside a ZIP, e-mail or attachment-bearing PDF | `parserails extract bundle.zip` |
| Decide whether OCR is needed before paying for it | `parserails is-complex doc.pdf` |
| Give a vision model a page image | `parserails render --dpi 150 doc.pdf` |

Every command takes `-` to read standard input.

## Reading a document

```bash
parserails parse report.pdf                        # plain text, lines intact
parserails parse --format markdown report.pdf      # headings, tables, lists
parserails parse --pages 1-5 --format json r.pdf   # words with boxes
```

Markdown is the right default when the text is going to a model: it keeps the
heading hierarchy and table structure that a flat dump loses.

## Scanned documents

Text extraction returns nothing for a scan — there is no text layer to
extract. Check first, then OCR only what needs it:

```bash
parserails is-complex scan.pdf          # exits 2 when any page needs OCR
parserails parse --ocr tesseract scan.pdf
```

`--image-ocr` additionally reads figures on pages that *do* have text (charts,
pasted screenshots), merging the result with the native words.

## Files inside files

```bash
parserails extract invoice-bundle.zip              # tree view
parserails extract --format text mail.eml          # everything, depth first
```

It walks PDF attachments, ZIP entries, e-mail and Outlook attachments, OLE
objects embedded in office documents, and whatever is inside those. Add
`--list` for an inventory without parsing.

## Interpreting the output

- **Text output** separates pages with a form feed (`\f`) and keeps line
  breaks, so page boundaries survive chunking.
- **JSON output** gives every word a page, a box in PDF points (origin
  bottom-left), and a `confidence` that is non-zero only for OCR'd words.
- **`is-complex`** prints per-page JSON with a `reasons` list —
  `scanned`, `no-text`, `sparse-text`, `embedded-images`, `garbled`.
- Markdown reconstruction is heuristic. Dense tables and unusual layouts
  render imperfectly; check the output before relying on a table.

## Limits worth knowing

- Reconstructed structure is inferred from geometry, not from the PDF's own
  tags. `--native-office` reads DOCX/XLSX/PPTX structure as authored instead,
  but has no page coordinates.
- `--timeout 30s` abandons a document that takes too long, so one bad file
  cannot stall a batch.
- Encrypted documents need `--password`.
