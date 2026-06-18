# Roadmap

ParseRails ships a focused core first — **spatial PDF text extraction** — and
grows toward full liteparse parity. Status as of the current release:

| Capability | Status |
|------------|--------|
| PDF text + word/char bounding boxes (PDFium WASM) | ✅ Done |
| Page dimensions, per-page word grouping | ✅ Done |
| Word vs. line granularity (`WithGranularity`) | ✅ Done |
| Opt-in font size (`WithFontInfo`) | ✅ Done |
| Page screenshot rendering (`RenderPage`, DPI) | ✅ Done |
| Pluggable OCR interface | ✅ Done |
| OCR fallback wiring (empty page → render → OCR) | ✅ Done |
| Tesseract CLI OCR adapter (cgo-free) | ✅ Done |
| HTTP OCR adapter (remote servers) | ✅ Done |
| Office formats (DOCX/PPTX/XLSX/...) via LibreOffice | ✅ Done |
| `ParseFile` + concurrent `ParseFiles` batch | ✅ Done |
| CLI (`parserails parse/render`, `go install`) | ✅ Done |
| Render by pixel size (not just DPI) | ⏳ Planned |
| Native cgo build mode (hot paths) | 🗺️ Considering |
| Native XLSX cell extraction (without LibreOffice) | 🗺️ Considering |

Legend: ✅ implemented · ⏳ planned, next up · 🗺️ considering.

## Design principles

- **cgo-free by default.** Every planned adapter (including Tesseract) must work
  without `CGO_ENABLED=1` — by subprocess or HTTP, not by linking C.
- **Delegate correctness.** Lean on PDFium for glyph positioning rather than
  re-implementing PDF text layout.
- **Built-in first, extensible on demand.** Ship a solid default set before
  adding plugin slots.
