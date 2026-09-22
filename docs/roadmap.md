# Roadmap

ParseRails started as **spatial PDF text extraction** and has grown into a
document front end: detection, structure, routing, containers and OCR around
the same cgo-free PDFium core. Status as of the current release:

## PDF core

| Capability | Status |
|------------|--------|
| PDF text + word/char bounding boxes (PDFium WASM) | ✅ Done |
| Page dimensions, per-page word grouping | ✅ Done |
| Word vs. line granularity (`WithGranularity`) | ✅ Done |
| Opt-in font size (`WithFontInfo`) | ✅ Done |
| Reading-order line reconstruction (`Lines`, `Text`) | ✅ Done |
| Encrypted documents, page ranges (`ReadOptions`) | ✅ Done |
| Page screenshot rendering (`RenderPage`, DPI) | ✅ Done |
| `ExtractText` whole-page text fast path | ✅ Done |
| Native cgo backend (`-tags parserails_cgo`) | ✅ Done |
| Render by pixel size (not just DPI) | ⏳ Planned |

## Structure

| Capability | Status |
|------------|--------|
| Block classification (headings, paragraphs, lists, tables, figures) | ✅ Done |
| Markdown output | ✅ Done |
| Column detection and banding | ✅ Done |
| Running header/footer removal | ✅ Done |
| Ruled-table detection from vector graphics | 🗺️ Considering |
| Tagged-PDF structure tree, annotations, form fields | 🗺️ Considering |

## Formats and containers

| Capability | Status |
|------------|--------|
| Magic-byte format detection (`Sniff`, `Detect`) | ✅ Done |
| Office formats via LibreOffice | ✅ Done |
| Native DOCX/XLSX/PPTX reading, no LibreOffice | ✅ Done |
| Standalone images (PNG/JPEG/TIFF/BMP/WebP/GIF) | ✅ Done |
| Recursive extraction: ZIP, EML, MSG, OLE, PDF attachments | ✅ Done |
| Pluggable containers (`WithContainer`) | ✅ Done |
| ODT/ODS/ODP read natively | 🗺️ Considering |
| Legacy `.doc`/`.xls` text without LibreOffice | 🗺️ Considering |

## OCR and routing

| Capability | Status |
|------------|--------|
| Pluggable OCR interface + whole-page fallback | ✅ Done |
| Tesseract CLI adapter (cgo-free) | ✅ Done |
| HTTP adapter speaking the LiteParse OCR API | ✅ Done |
| Per-word confidence (`Word.Confidence`) | ✅ Done |
| Figure OCR on text pages, merged with native words (`WithImageOCR`) | ✅ Done |
| Complexity inspection (`Inspect`, `is-complex`) | ✅ Done |
| Layout-complexity signals (multi-column, table-likely) in `Inspect` | ⏳ Planned |

## Operations

| Capability | Status |
|------------|--------|
| `ParseFile` + concurrent `ParseFiles` batch | ✅ Done |
| Per-document timeouts (`WithTimeout`) | ✅ Done |
| CLI: parse, batch, extract, render, is-complex, stdin | ✅ Done |
| Agent skill (`SKILL.md`) | ✅ Done |
| Throughput benchmarks vs. Go PDF readers | ✅ Done |
| Quality benchmark runner (olmOCR-bench et al.) | ✅ Harness only |
| Published quality numbers | ⏳ Planned |
| Worker isolation in a subprocess (hard kill, not abandon) | 🗺️ Considering |

Legend: ✅ implemented · ⏳ planned, next up · 🗺️ considering.

## Design principles

- **cgo-free by default.** Every adapter must work without `CGO_ENABLED=1` —
  by subprocess or HTTP, not by linking C.
- **Delegate correctness.** Lean on PDFium for glyph positioning rather than
  re-implementing PDF text layout, and on the document's own structure
  wherever it declares one.
- **Degrade, don't fail.** A missing dependency, an unreadable attachment or a
  backend without an experimental API should cost you that one thing, not the
  batch.
- **Built-in first, extensible on demand.** Ship a solid default set before
  adding plugin slots — then add the slot (`WithOCR`, `WithContainer`).
