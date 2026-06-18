# Examples

Runnable examples live under [`examples/`](https://github.com/promptrails/parserails/tree/main/examples).
Each is its **own Go module** (with a `replace` pointing at the local source), so
their dependencies never touch the core module's graph — and each ships a
`Dockerfile` you can build as-is.

| Example | Shows | Backend | Image deps |
|---------|-------|---------|------------|
| [extract-text](https://github.com/promptrails/parserails/tree/main/examples/extract-text) | text + word boxes, the basics | WASM | none (static) |
| [parse-server](https://github.com/promptrails/parserails/tree/main/examples/parse-server) | HTTP service, OCR fallback, office docs | WASM | tesseract, libreoffice |
| [native-cgo](https://github.com/promptrails/parserails/tree/main/examples/native-cgo) | native PDFium for max throughput | cgo | libpdfium |

## extract-text

The smallest possible program — open a PDF, print text and a few boxes. Builds to
a fully static, cgo-free binary on `distroless/static`.

```bash
cd examples/extract-text
go run . sample.pdf
```

## parse-server

A `net/http` service: `POST /parse` a PDF or office document, get spatial text as
JSON. Tesseract OCR covers scanned pages; LibreOffice converts DOCX/PPTX/XLSX.

```bash
cd examples/parse-server
go run .
curl -F file=@invoice.pdf localhost:8080/parse
```

## native-cgo

The high-throughput path — built with `-tags parserails_cgo`, linking libpdfium.
The right backend for a Dockerized ingestion worker that wants poppler-class
speed while keeping OCR, office, and spatial features.

```bash
cd examples/native-cgo
CGO_ENABLED=1 go run -tags parserails_cgo . sample.pdf   # needs libpdfium
```

See [Architecture → Backends](architecture.md) for the WASM-vs-cgo trade-off.
