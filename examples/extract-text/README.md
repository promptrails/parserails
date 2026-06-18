# extract-text

The simplest ParseRails example — open a PDF and print its text plus a few word
bounding boxes, using the default cgo-free WASM backend.

```bash
go run . sample.pdf
```

```
backend=wasm pages=1 words=42

p0 "ParseRails"          [73 739 251 748]
p0 "Benchmark"          ...
...
```

## Docker

The [Dockerfile](./Dockerfile) builds a fully static binary with `CGO_ENABLED=0`
on `distroless/static` — no system libraries, because PDFium ships as WASM.

```bash
docker build -t parserails-extract .
docker run --rm -v "$PWD:/data" parserails-extract /data/sample.pdf
```
