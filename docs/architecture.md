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

The trade-off is throughput: WASM is somewhat slower than a native cgo build of
PDFium. For most document workloads this is well worth the operational
simplicity. A native cgo build mode may be offered later for hot paths.

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
