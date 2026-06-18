# Roadmap

ParseRails ships a focused core first — **spatial PDF text extraction** — and
grows toward full liteparse parity. Status as of the current release:

| Capability | Status |
|------------|--------|
| PDF text + word/char bounding boxes (PDFium WASM) | ✅ Done |
| Page dimensions, per-page word grouping | ✅ Done |
| Pluggable OCR interface | ✅ Interface done |
| Page screenshot rendering (DPI / pixels) | ⏳ Planned |
| OCR fallback wiring (empty page → render → OCR) | ⏳ Planned |
| Tesseract CLI OCR adapter (cgo-free) | ⏳ Planned |
| HTTP OCR adapter | ⏳ Planned |
| Office formats (DOCX/XLSX/PPTX) via LibreOffice + excelize | 🗺️ Roadmap |
| CLI (`parserails parse file.pdf`) | 🗺️ Roadmap |
| Batch parsing + concurrency helpers | 🗺️ Roadmap |
| Native cgo build mode (hot paths) | 🗺️ Considering |

Legend: ✅ implemented · ⏳ planned, next up · 🗺️ on the roadmap, not started.

## Design principles

- **cgo-free by default.** Every planned adapter (including Tesseract) must work
  without `CGO_ENABLED=1` — by subprocess or HTTP, not by linking C.
- **Delegate correctness.** Lean on PDFium for glyph positioning rather than
  re-implementing PDF text layout.
- **Built-in first, extensible on demand.** Ship a solid default set before
  adding plugin slots.
