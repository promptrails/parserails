# Troubleshooting

Start with the input format, the requested output, and the actual error.
Recognition, spatial parsing and recursive extraction have different scopes;
the [Usage Guide](usage-guide.md) maps each task to its entry point.

## A new feature or documentation page is missing

`go install ...@latest` uses a published version. To test a checked-out branch,
build its CLI with `go build -o ./bin/parserails ./cmd/parserails` and run that
binary explicitly. Do not assume the installed executable follows your checkout.

The documentation site deploys from `docs/` on pushes to **main**, when docs or
the docs workflow changed. Opening or updating a PR does not publish its pages.
Review the Markdown in that branch, or preview from the repository root:

```bash
python3 -m http.server 8000 --directory docs
```

Open `http://localhost:8000/` in a browser. Docsify loads Markdown at runtime,
and the sidebar comes from `_sidebar.md`. The page shell loads its JavaScript,
styles and fonts from external CDNs, so a fully offline browser will not render
the same preview. Publishing does not require generating a separate HTML file
for each Markdown page.

## Office parsing says LibreOffice is missing

Spatial `ParseData`/`ParseFile` and `ParseFiles` require LibreOffice for Office
inputs. Configure `WithLibreOffice`, `PARSERAILS_SOFFICE`, or put `soffice` /
`libreoffice` on PATH. Match the missing-binary error with
`errors.Is(err, parserails.ErrNoLibreOffice)`.

If you only need DOCX/XLSX/PPTX text or Markdown, use the native reader or
`parse --native-office`. `parse --native-office --format json` is rejected
because the native reader cannot provide page coordinates. The CLI batch JSON
path instead uses spatial parsing and still needs LibreOffice.

Native Office fallback occurs in text extraction and the container walk when
the binary is missing; it does not mask arbitrary conversion errors and is not
a fallback for spatial parsing. See [Office Formats](office.md).

## Parsing a ZIP or email says it cannot parse the content

Use `extract`, `Extract`, or `ExtractFile`. `parse` produces a spatial document,
while ZIP, EML and MSG need the container walk. A legacy OLE signature cannot
distinguish DOC/XLS/PPT/MSG on its own; supply a meaningful file name through
`ReadOptions.Name` when reading bytes.

## Text is empty or an OCR candidate was not recognized

`ExtractText` on PDFs reads the text layer only. It never runs OCR. Use `Parse`
or `ParseData` with `WithOCR`, or `parse --ocr tesseract`. Standalone images
require an OCR backend; PDFs without one can yield empty pages.

The regular PDF OCR fallback runs only on pages with zero words. `Inspect`
can flag sparse/garbled text or embedded figures, but it does not change the
fallback's trigger. Enable `WithImageOCR` for figures on text pages, or render
and explicitly send a page to your OCR backend when your policy requires it.
Check Tesseract's installed language data or the remote server's language code.

## Attachment extraction succeeds but is incomplete

Inspect `Node.Err` throughout the tree, including the root. CLI `extract` can
exit zero with node errors; JSON/tree output preserves those errors, plain text
does not. Check [quota accounting](limits.md), duplicate-content notices,
dependency errors and the depth setting. A node at the depth limit is read but
its contents are not traversed, without an additional depth-error marker.

An image with no OCR backend or another unsupported leaf can appear in the
inventory without text. `--list` intentionally skips text parsing. Embedded
MSG messages become `.txt` children rather than reconstructed `.msg` files;
`extract` does not save original attachment bytes.

## Markdown tables or image links look wrong

PDF layout is inferred from coordinates. Use native Office reading when you
have an original OOXML document and need authored table structure. Sparse
XLSX data may be compacted to keep memory bounded. Formula results are read
from stored values; formulas are not recalculated.

Markdown figure links are placeholders, not saved image files. Render/crop
figures separately and supply those assets. Running headers and footers are
removed by default; use `--keep-headers-footers` or `MarkdownWith` to retain
them. See [Blocks & Markdown](markdown.md).

## A timeout returns but memory or worker usage stays high

The timeout stops waiting for a PDFium operation; it cannot kill the engine
call already running. Also, `MaxBytes` is an extraction-content budget rather
than a process memory cap. PDF attachments may be materialized before their
size can be checked. See [Limits & Timeouts](limits.md) for scope and worker
isolation options.

## CLI flags appear to be ignored

Place flags before file/directory arguments: `parserails parse --format json
file.pdf`. Flags after the positional arguments are not parsed as options.
Use `parserails <command> --help` for that command's flags. `--pages` is 1-based;
`render --page`, result page indexes and `OCRPages()` are 0-based.
