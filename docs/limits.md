# Limits & Timeouts

ParseRails has several independent limits. Extraction quotas constrain a
container walk; page limits constrain PDF work; deadlines bound how long a
caller waits. None is a process-wide memory or CPU limit.

## Extraction quotas

```go
node, err := p.Extract(ctx, data, parserails.ExtractOptions{
	ReadOptions: parserails.ReadOptions{Name: "bundle.zip"},
	MaxDepth:    4,
	MaxFiles:    100,
	MaxBytes:    64 << 20,
})
```

| Option | Zero-value behavior | Accounting |
|---|---|---|
| `MaxDepth` | 8 | root is depth 0; a node at the limit is read but its children are not enumerated |
| `MaxFiles` | 512 | root, accepted children and attempted-but-refused files consume slots |
| `MaxBytes` | 256 MiB | root bytes plus the contents of accepted children, across all levels |
| Per-child read cap | 128 MiB | internal cap used by the built-in readers; not a public option |

Use a negative `MaxDepth` to disable the depth cap. Use positive file and byte
limits; zero selects defaults for `ExtractOptions`. Do not confuse this with
a custom container's `ChildRequest`: there, zero means **no limit**, because
the walker supplies remaining budgets rather than user defaults.

The root consumes a file slot and its byte length. If a 1 MiB ZIP contains a
2 MiB nested ZIP and a 4 MiB text file inside that ZIP, those accepted files
consume 7 MiB in total. The budget is not just the final text size or just the
compressed input size. Siblings are reserved before recursion, so descending
into one child cannot reuse the bytes or slots held by the others.

A failed attempt consumes a slot even if it produces no data. A stop notice
reporting that no quota remains is not another attempted file. Consequently
`Node.Count()` can exceed `MaxFiles`: it includes those error/stop nodes.
Reaching the depth cap does not itself produce a stop node.

For MIME attachments, bytes are measured after transfer decoding. Embedded
Outlook messages are emitted as text, and their byte charge includes rendered
headers and separators. Their property streams also have bounded reads that
allow UTF-16 encoding overhead. One embedded message uses one file slot,
regardless of the number of its properties. Attachments are processed in
storage-name order, finishing one before reading the next.

The CLI exposes depth and file limits. It currently has **no `--max-bytes`
flag** and uses the 256 MiB extraction default; use the Go API for a different
byte budget.

## What the byte budget does not guarantee

`MaxBytes` is not a ceiling on Go heap size, PDFium memory, decoded image
pixels or all temporary allocations. The input file is read before the
extraction walk checks its size. Container metadata, decoding, rendering and
parsed document structures require additional memory.

PDFium returns an embedded PDF attachment as one complete buffer. ParseRails
checks its size **after** that allocation, then reports and drops it if it is
too large. An oversized PDF attachment can therefore exceed the configured
budget in memory before being refused. `SkipParse` does not avoid attachment
enumeration or that allocation.

For untrusted uploads, enforce request/input size limits in the host
application. If a strict memory ceiling or hard termination is required,
run parsing in a separately limited worker process. The library does not
provide process isolation.

## Pages and decoded images

`ReadOptions.Pages` selects PDF pages; `MaxPages` caps the selected set.
Zero `MaxPages` inherits `WithMaxPages`, whose default is unlimited. A negative
per-read `MaxPages` removes that default cap.

The standalone image path checks dimensions before pixel decoding and rejects
images above `100 << 20` pixels. It decodes one image frame, not an entire
animated GIF or a multi-page TIFF sequence. This guard is independent of
container byte limits and PDF page rendering.

Native XLSX reading limits dense-grid expansion to `1 << 20` cells and avoids
expanding very sparse data into a huge rectangle. Sparse sheets may be
compacted or left ragged; blank-column alignment is not guaranteed under
that fallback. These protections do not make the native Office reader a
streaming parser. See [Office Formats](office.md).

## Per-operation and whole-request deadlines

```go
p, err := parserails.New(parserails.WithTimeout(30 * time.Second))
if err != nil {
	return err
}
defer p.Close()

ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
defer cancel()
node, err := p.ExtractFile(ctx, "bundle.zip", parserails.ExtractOptions{})
```

`WithTimeout(0)` is the default and adds no timeout. A positive value bounds
individual operations such as PDF parsing, conversion, inspection, rendering,
image OCR, native Office text reading through the parser, and container
enumeration. Within those operations, an earlier caller deadline wins.

Several operations can happen in sequence: Office conversion then PDF parsing,
or a separate parse and enumeration for each child in a tree. `WithTimeout`
is therefore **not one total deadline for `Extract` or `ParseFiles`**. Use a
context deadline to bound the whole request, as above. File reads and document
post-processing are not all covered by the operation timeout.

The standalone `ReadOfficeDocument` function takes no context or timeout.
Currently, CLI `parse`/`batch` with `--native-office` call that function directly
for native text/Markdown, so `--timeout` does not bound that path. Library
`ExtractTextData`, `ExtractFileText` and `Extract` with `WithNativeOffice`
use the parser's bounded native-text operation.

## Cancellation is not a hard kill

An in-progress PDFium call cannot be interrupted. When a bounded call times
out, the caller stops waiting; its worker remains busy until the engine call
returns and cleanup completes. There is no guarantee that a pathological
engine call will finish promptly. Enough stuck calls can occupy the whole pool.

LibreOffice runs as a context-bound subprocess and can be killed. HTTP OCR
requests use the context; custom OCR/container implementations should observe
it too. The default HTTP OCR client separately has a 60-second timeout, which
can be replaced through `httpocr.Config.Client`.

Matching a caller cancellation with `errors.Is(err, context.Canceled)` or
`context.DeadlineExceeded` is appropriate when that error reaches the caller.
The parser's own timeout produces a descriptive error; it has no exported
timeout sentinel. During extraction, failures can also be stored in
`Node.Err`, so inspect the tree as well as the returned error.

## Partial results and duplicate content

`Extract` normally records unreadable or refused files as nodes and continues
with siblings. `AllText()` contains the recovered text but not an error report;
use `Walk` or JSON to record failures. Unsupported content and images without
an OCR backend can remain as inventory nodes with no text. No-error alone does
not guarantee a nonempty textual result.

Content hashes are tracked across the whole walk. Identical bytes under two
different names are read once; later occurrences carry an `already extracted
this file` error. Their slots/bytes have already been reserved. This prevents
repeated content from being parsed indefinitely; it is not a promise to return
text for every duplicate attachment.
