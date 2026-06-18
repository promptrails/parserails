# parse-server

A small HTTP service on ParseRails: POST a document, get its spatial text as
JSON. Demonstrates the OCR fallback (Tesseract) and office-document support
(LibreOffice) together.

```bash
go run .                                   # listens on :8080 (ADDR to override)
curl -F file=@invoice.pdf  localhost:8080/parse
curl -F file=@report.docx  localhost:8080/parse   # office → LibreOffice → parse
curl -F file=@scanned.pdf  localhost:8080/parse   # image-only → Tesseract OCR
```

Response:

```json
{
  "pages": [
    { "index": 0, "width": 612, "height": 792,
      "words": [ { "text": "Invoice", "page": 0, "x0": 72, "y0": 709, "x1": 110, "y1": 718 } ] }
  ]
}
```

| Env | Default | Purpose |
|-----|---------|---------|
| `ADDR` | `:8080` | listen address |
| `OCR_LANG` | `eng` | Tesseract language |

## Docker

PDF parsing is cgo-free; the runtime image only adds `tesseract-ocr` (OCR) and
`libreoffice-*` (office conversion) — the optional binaries those features call.

Build from the repo root (so the `replace` directive resolves locally):

```bash
docker build -f examples/parse-server/Dockerfile -t parserails-server .
docker run --rm -p 8080:8080 parserails-server
```
