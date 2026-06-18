# native-cgo

Runs ParseRails on the **native PDFium backend** (`-tags parserails_cgo`), which
links `libpdfium` directly instead of running it as WebAssembly. This is the
high-throughput, low-memory path for controlled hosts — e.g. a Dockerized
document-ingestion worker that wants poppler-class speed without giving up
ParseRails' OCR, office, and spatial features.

```bash
# needs libpdfium + pkg-config on the host
CGO_ENABLED=1 go run -tags parserails_cgo . sample.pdf
```

```
backend=cgo chars=512 elapsed=2.1ms

ParseRails Benchmark Document
This is a small single page PDF ...
```

Build without the tag and you get the default cgo-free WASM backend
(`backend=wasm`) — same code, no libpdfium required.

## Docker

The [Dockerfile](./Dockerfile) installs prebuilt
[libpdfium](https://github.com/bblanchon/pdfium-binaries), writes a `pdfium.pc`
for `pkg-config`, and builds with `CGO_ENABLED=1 -tags parserails_cgo`.

Build from the repo root (so the `replace` directive resolves locally — no
publishing required):

```bash
docker build -f examples/native-cgo/Dockerfile -t parserails-native .
docker run --rm -v "$PWD:/data" parserails-native /data/sample.pdf
```

> If you run `go mod tidy` here, it evaluates default build tags and prunes the
> cgo-only modules. Restore them with:
> `go get github.com/hashicorp/go-hclog github.com/hashicorp/go-plugin`
