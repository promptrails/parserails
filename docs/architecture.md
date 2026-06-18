# Architecture

ParseRails is a thin, opinionated layer over Google's PDFium engine.

## The engine: PDFium via WebAssembly

PDFium is the C++ PDF library behind Chrome's PDF viewer — the same engine
[liteparse](https://github.com/run-llama/liteparse) uses. ParseRails reaches it
through [`klippa-app/go-pdfium`](https://github.com/klippa-app/go-pdfium), running
PDFium compiled to **WebAssembly** under the [wazero](https://wazero.io) runtime.

This is the key design choice:

- **No cgo.** `CGO_ENABLED=0` builds work. Cross-compilation is trivial.
- **No system libraries.** Nothing to `apt-get install` in your Docker image.
- **Deterministic.** The WASM module is the same on every platform.

The trade-off is throughput: WASM is meaningfully slower and heavier than a
native build of PDFium — most of the cost is PDFium loading each page inside the
WASM sandbox, which is irreducible. For most document workloads this is well
worth the operational simplicity. When you need text-only output (no boxes), the
`ExtractText` fast path uses PDFium's whole-page text API (one call per page).

## Backends: WASM (default) vs. native cgo

ParseRails has two interchangeable backends selected at build time:

| | default | `-tags parserails_cgo` |
|---|---------|------------------------|
| Engine | PDFium as WebAssembly (wazero) | PDFium linked natively |
| cgo | none | required |
| System deps | none | `libpdfium` + `pkg-config` + C toolchain |
| Speed / memory | portable, slower | much faster, far lighter |
| `parserails.Backend` | `"wasm"` | `"cgo"` |

The API is identical; only the build tag and `Backend` constant differ.

```bash
go build .                          # WASM, cgo-free (default)
go build -tags parserails_cgo .     # native, needs libpdfium
```

Use the default everywhere for portability; switch to the cgo backend on
performance-critical, controlled hosts (e.g. a Dockerized ingestion worker) where
installing libpdfium is fine and throughput matters.

> The cgo backend pulls extra modules (`hashicorp/go-plugin`, `go-hclog`) used
> only under the build tag. A plain `go mod tidy` evaluates default tags and will
> prune them; if you tidy, restore with
> `go get github.com/hashicorp/go-hclog github.com/hashicorp/go-plugin`.

## The pool

`New()` initializes a pool of PDFium worker instances. Each `Parse` call borrows
an instance and returns it when done, so a single `Parser` is safe for concurrent
use. Size the pool to your workload with `WithPoolSize`.

```
Parser ──► pdfium.Pool ──► [worker] [worker] [worker]   (wazero instances)
   │
   └─ Parse(ctx, pdf)  borrow → OpenDocument → per-page extract → return
```

## Layering

The package follows the PromptRails service conventions so it drops cleanly into
a standalone parse service:

```
domain types  →  parser  →  ocr  →  (service / handler in the host app)
```

- **domain** — `Word`, `Page`, `Document` (`types.go`)
- **parser** — the PDFium-backed extractor (`parser.go`)
- **ocr** — the pluggable `OCR` interface (`ocr.go`)

Host applications add the transport (HTTP handler, queue worker) on top.

## Why a separate engine instead of pure Go?

Pure-Go PDF readers exist, but PDF text positioning is hard: glyph placement,
font metrics, transformation matrices, and rotation all have to be correct to get
trustworthy bounding boxes. PDFium already does this correctly and is battle-
tested in Chrome. ParseRails borrows that correctness and pays for it only in
WASM overhead — not in cgo pain. See the [Benchmarks](benchmarks.md) for how this
compares to pure-Go readers.
