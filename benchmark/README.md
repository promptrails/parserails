# ParseRails Benchmarks

Throughput comparison of ParseRails against popular Go PDF libraries on the same
inputs.

This is a **separate Go module** (its own `go.mod`, wired to the parent with a
`replace` directive). The competitors pull in large — and in unipdf's case
AGPL/commercial — dependency trees, so isolating them here keeps the main
`parserails` module's dependency graph clean. Nothing here ships with the library.

## Run it

```bash
cd benchmark
go test -bench=. -benchmem ./...
```

Regenerate the sample PDFs (pure stdlib, no deps):

```bash
go run testdata/gen.go
```

## What's measured

| Library | What its call does | Returns boxes? | License |
|---------|--------------------|----------------|---------|
| **parserails** | PDFium (WASM) char-level extraction → grouped words | **Yes** | MIT (PDFium: Apache-2.0) |
| [ledongthuc/pdf](https://github.com/ledongthuc/pdf) | pure-Go plain-text extraction | No | BSD-3 |
| [pdfcpu/pdfcpu](https://github.com/pdfcpu/pdfcpu) | raw content-stream extraction¹ | No | Apache-2.0 |
| [unidoc/unipdf](https://github.com/unidoc/unipdf) | plain-text extraction | No | AGPL-3.0 / commercial² |

> ¹ **pdfcpu has no plain-text extraction.** `ExtractContent` returns the decoded
> content streams (raw PDF operators), not readable text. It is measuring a
> lighter, different operation and is included only for reference.
>
> ² **unipdf requires a license key** for text extraction. Without one it prints
> "Unlicensed copy of UniPDF" and extraction fails, so the benchmark **skips** it.
> Set a key via `license.SetMeteredKey(...)` to include it.

## Results

`darwin/arm64`, Apple M3 Pro, Go 1.26. Inputs: `small.pdf` (1 page, ~11 lines),
`report.pdf` (8 pages, ~36 lines/page).

| Benchmark | Input | ns/op | B/op | allocs/op |
|-----------|-------|------:|-----:|----------:|
| ParseRails | small | 4,826,519 | 70 MB | 65,095 |
| ParseRails | report | 129,039,708 | 1.9 GB | 1,938,326 |
| ledongthuc | small | 25,752 | 61 KB | 438 |
| ledongthuc | report | 417,647 | 699 KB | 6,563 |
| pdfcpu¹ | small | 33,604 | 69 KB | 304 |
| pdfcpu¹ | report | 140,582 | 293 KB | 1,232 |
| unipdf² | — | skipped (unlicensed) | — | — |

## How to read this

**ParseRails is far slower and far more allocation-heavy here — and that is the
expected trade-off, not a bug.** Two reasons:

1. **It returns something the others don't: per-word bounding boxes.** ledongthuc
   and unipdf give you a flat string; pdfcpu gives raw operators. ParseRails
   extracts every character with its position and groups words. This is a
   different, heavier job — spatial extraction, not plain text.
2. **PDFium runs as WebAssembly (cgo-free).** Every character crosses the
   Go↔WASM boundary, which dominates the allocations. The cost buys operational
   simplicity: no cgo, no native libs, trivial cross-compilation.

So the honest summary:

- **Need only flat text, want raw speed, cgo is fine?** A pure-Go reader like
  ledongthuc is one to two orders of magnitude faster.
- **Need positions / bounding boxes, the same engine Chrome uses, and a
  cgo-free build?** That's what ParseRails is for, and these are its costs.

### Known optimization target

The `report.pdf` allocation figure (1.9 GB/op) is dominated by marshaling
char-level structured text across the WASM boundary. Switching the hot path to
PDFium's rect-level extraction, or batching the boundary crossings, is a clear
future optimization — see the project roadmap. The numbers above are the honest
*current* state, unoptimized.
