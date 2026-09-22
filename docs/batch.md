# Batch Processing

Use a batch for a directory of independent PDFs, Office documents or images.
Use [recursive extraction](containers.md) for the contents of ZIP, EML, MSG or
document attachments. These are separate operations.

## Command-line workflow

```bash
parserails batch --format markdown --concurrency 4 ./input ./output
parserails batch --format json --ext .pdf --recursive=false ./input ./output
parserails batch --native-office --format markdown --ext .docx ./input ./output
parserails batch --ocr tesseract --lang eng --timeout 30s ./scans ./text
```

Put all flags before the two directories. `--recursive` defaults to true,
`--format` to `text`, and `--concurrency` to the CPU count. The CLI clamps
nonpositive concurrency to one. The full flag list is in the [CLI reference](cli.md).

Input discovery uses **file extensions**: PDF, supported Office extensions,
and supported image extensions. ZIP/EML/MSG/plain-text files are skipped.
`--ext .pdf` narrows the selection; it does not add support for an otherwise
unsupported extension. A PDF named `invoice.dat` will not be discovered by
`batch`, although a direct `parse invoice.dat` can detect its content.

An empty selection is an error. Use a separate output directory outside the
input tree so previous output files are not discovered on the next run.

## Output layout and collisions

The relative input path is preserved, with the output suffix selected by
format: `.txt`, `.md`, or `.json`.

```text
input/2026/invoice.pdf  -> output/2026/invoice.md
input/2026/letter.docx  -> output/2026/letter.md
```

When two selected inputs have the same stem, their original extensions are
retained:

```text
input/report.pdf       -> output/report.pdf.txt
input/report.docx      -> output/report.docx.txt
```

If that still collides with another planned name, numeric suffixes such as
`-2` are added. Planning prevents two workers in **one invocation** from
writing the same output. It does not provide locking between separate batch
processes or preserve output files from previous runs: existing files at the
chosen paths are truncated and replaced.

## Successes, errors, and exit codes

By default, successful output paths go to stdout; failures and a final count
go to stderr. `-q` suppresses success paths and the count, while keeping errors.
Output order follows completion order and can vary between runs.

```bash
if parserails batch -q --format markdown ./input ./output 2>batch-errors.log; then
  echo "All selected documents were written"
else
  echo "At least one document failed; inspect batch-errors.log"
fi
```

A failed document does not cancel the remaining files. The command exits
nonzero after the batch if any file failed. Outputs from successful files stay
on disk. There is no built-in retry, resume manifest or failed-files-only mode;
choose the inputs for a retry in your application or a separate directory.

The CLI exposes a per-operation `--timeout`, but not a deadline for the whole
batch. See [Limits & Timeouts](limits.md), including the current limitation
of the CLI's direct native Office path.

## Go library

Pass an explicit list of paths and a positive concurrency value. Unlike the
CLI, `ParseFiles` does not discover or filter a directory.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/promptrails/parserails"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: go run . <file> [file...]")
	}
	p, err := parserails.New(
		parserails.WithPoolSize(1, 4, 4),
		parserails.WithTimeout(30*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for _, result := range p.ParseFiles(ctx, os.Args[1:], 4) {
		if result.Err != nil {
			log.Printf("%s: %v", result.Path, result.Err)
			continue
		}
		fmt.Printf("%s: %d pages\n", result.Path, len(result.Document.Pages))
	}
}
```

`[]FileResult` has the **same order as the input paths**, even when documents
finish out of order. Inspect each `FileResult.Err` before using `Document`.
No output files are created by the library.

`ParseFiles(..., concurrency <= 0)` means **one concurrent call per input**,
not the CLI's clamp-to-one behavior. Set a positive value for large corpora.
The PDFium pool separately bounds engine workers; extra calls may wait for an
instance. Office conversion and OCR also consume resources, so increase
concurrency only after measuring your workload.

`ParseFiles` uses `ParseFile` and returns spatial `Document` values. Therefore
`WithNativeOffice()` does not remove its LibreOffice requirement for Office
inputs. To batch native OOXML text in Go, use your own bounded worker loop
around `ExtractFileText` or `ReadOfficeDocument`. See [Office Formats](office.md).
