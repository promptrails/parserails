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

| Benchmark | Input | ns/op | B/op | allocs/op | Boxes |
|-----------|-------|------:|-----:|----------:|:-----:|
| ParseRails — word | small | 2,916,885 | 38 MB | 40,060 | per-word |
| ParseRails — word | report | 63,382,097 | 737 MB | 993,989 | per-word |
| ParseRails — line | small | 1,629,575 | 19 MB | 13,641 | per-line |
| ParseRails — line | report | 19,170,561 | 29 MB | 19,154 | per-line |
| ledongthuc | small | 25,150 | 61 KB | 438 | none |
| ledongthuc | report | 441,889 | 699 KB | 6,563 | none |
| pdfcpu¹ | small | 33,352 | 69 KB | 304 | none |
| pdfcpu¹ | report | 145,337 | 293 KB | 1,232 | none |
| unipdf² | — | skipped (unlicensed) | — | — | none |

`word` = `GranularityWord` (default, per-word boxes). `line` =
`GranularityLine` (PDFium text rectangles, per-line boxes).

## How to read this

**ParseRails is slower and more allocation-heavy here — and that is the expected
trade-off, not a bug.** Two reasons:

1. **It returns something the others don't: bounding boxes.** ledongthuc and
   unipdf give you a flat string; pdfcpu gives raw operators. ParseRails reports
   positioned text. That is a different, heavier job — spatial extraction, not
   plain text.
2. **PDFium runs as WebAssembly (cgo-free).** Text crosses the Go↔WASM boundary,
   which dominates the allocations. The cost buys operational simplicity: no cgo,
   no native libs, trivial cross-compilation.

So the honest summary:

- **Need only flat text, want raw speed, cgo is fine?** A pure-Go reader like
  ledongthuc is one to two orders of magnitude faster.
- **Need positions / bounding boxes, the same engine Chrome uses, and a
  cgo-free build?** That's what ParseRails is for, and these are its costs.

### Optimizations applied

The first cut of ParseRails was ~2–3× heavier than the numbers above. Two changes
landed after this benchmark exposed the cost:

- **Font-info collection is now opt-in** (`WithFontInfo`). It roughly doubled
  cost because PDFium reports font metrics per character across the WASM
  boundary. Default off → `report` dropped from ~129 ms / 1.9 GB to ~63 ms /
  737 MB.
- **`GranularityLine` mode** uses PDFium's rect-level extraction, crossing the
  WASM boundary per *line* instead of per *character* — ~6× faster and ~25× fewer
  allocations than word mode, when per-line boxes are enough.

These are the honest *current* numbers. Re-run anytime with the commands above.
