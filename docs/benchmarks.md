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

## Speed is the easy half

Throughput says nothing about whether the output is usable: a reader that
returns a flat string beats every structured parser and loses every column,
table and heading on the way. What matters for a RAG or LLM pipeline is how
faithfully a document survives conversion, which is scored by public
doc→Markdown benchmarks against their own ground truth — olmOCR-bench,
opendataloader-bench and ParseBench.

[`benchmark/quality/`](https://github.com/promptrails/parserails/tree/main/benchmark/quality)
holds a runner that converts a corpus with this working tree's CLI, ready for
those harnesses to score:

```bash
cd benchmark/quality
./run.sh /path/to/corpus ./out --ocr tesseract
```

No quality numbers are checked in yet. Publishing a table means running every
tool at a pinned version on one machine; a table built any other way — mixing
leaderboard numbers with local runs, or scoring a tuned configuration against
other tools' defaults — is worse than no table.
