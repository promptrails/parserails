# Quality benchmarking

The benchmark next door measures **throughput** against other Go PDF readers.
Throughput is the easy half: a reader that returns a flat string beats every
structured parser and tells you nothing about whether the output is usable.

What matters for a RAG or LLM pipeline is how faithfully a document survives
conversion — reading order, heading hierarchy, table structure. That is scored
by public doc→Markdown benchmarks, against their own ground truth:

| Benchmark | What it scores |
|-----------|----------------|
| [olmOCR-bench](https://github.com/allenai/olmocr/tree/main/olmocr/bench) | per-category pass rates: multi-column, tables, headers/footers, old scans |
| [opendataloader-bench](https://github.com/opendataloader-project/opendataloader-bench) | reading-order similarity (NID), table structure (TEDS), heading hierarchy (MHS) |
| [ParseBench](https://github.com/run-llama/parse-bench) | tables, charts, content faithfulness, semantic formatting, visual grounding |

## Producing output to score

Each harness takes a directory of converted documents. `run.sh` produces one
with this working tree's CLI:

```bash
./run.sh /path/to/corpus ./out                      # no OCR
./run.sh /path/to/corpus ./out-tess --ocr tesseract # with OCR
```

Then run the benchmark's own evaluator over `./out`, unmodified. Use each
tool's default settings, including ParseRails': a comparison where only one
side was tuned is not a comparison.

## No numbers are checked in

There are no scores in this repository yet. Publishing a table means running
every tool at a pinned version on one machine, and a table produced any other
way — mixing leaderboard numbers with local runs, or scoring a tuned
configuration against others' defaults — is worse than no table at all.

What is known from the shape of the implementation, and what these benchmarks
would measure:

- **Reading order and columns** are reconstructed from geometry; multi-column
  pages are cut at the gutter and full-width lines band the page.
- **Tables** are found by column alignment, since PDFium reports no ruling
  lines. Dense and ruled tables are where heuristics fray, and TEDS is the
  number that will say by how much.
- **Headers and footers** are dropped when they repeat across pages, which
  olmOCR-bench rewards and which costs nothing on single-page documents.
- **Scans** depend entirely on the OCR backend; ParseRails contributes the
  routing ([`is-complex`](../../docs/complexity.md)) and the merge, not the
  recognition.

Contributions of reproducible runs — one machine, pinned versions, one
command — are welcome.
