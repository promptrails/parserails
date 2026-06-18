# Benchmarks

ParseRails is benchmarked against popular Go PDF readers on the same documents.
The benchmark lives in its **own Go module** under [`benchmark/`](https://github.com/promptrails/parserails/tree/main/benchmark)
so the competitors' (heavy) dependencies never enter the main module's graph.

## What's compared

| Library | Approach |
|---------|----------|
| **parserails** | PDFium via WebAssembly (this project) |
| [ledongthuc/pdf](https://github.com/ledongthuc/pdf) | pure-Go reader |
| [pdfcpu/pdfcpu](https://github.com/pdfcpu/pdfcpu) | pure-Go processor |
| [unidoc/unipdf](https://github.com/unidoc/unipdf) | commercial / AGPL reader |

> Note: bounding-box fidelity is not the same across these libraries — most
> pure-Go readers return flat text, while ParseRails returns positioned words.
> The benchmark measures text-extraction throughput on the same inputs; read it
> alongside the feature differences.

## Running it

```bash
cd benchmark
go test -bench=. -benchmem ./...
```

The benchmark module uses a `replace` directive to point at the local
ParseRails source, so it always benchmarks your working tree.

See [`benchmark/README.md`](https://github.com/promptrails/parserails/tree/main/benchmark)
for sample inputs, the latest numbers, and licensing notes (unipdf in particular).
