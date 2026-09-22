# Complexity: does this document need OCR?

`Inspect` answers, page by page, whether a document needs OCR — **before** you
commit to parsing it. It is a text-layer pass: page text, how much of the page
that text covers, and the rasters drawn on it. No page is rendered and no OCR
runs, so it costs a fraction of a parse.

Use it to route documents to a cheap or an expensive pipeline, to reject what
you cannot handle, or to price a batch by counting the pages that will really
need OCR.

```go
c, err := p.Inspect(ctx, data, parserails.ReadOptions{Name: "scan.pdf"})
if c.NeedsOCR() {
	fmt.Println("pages needing OCR:", c.OCRPages())
}
```

`InspectFile` is the same for a path.

## What each page reports

```go
type PageComplexity struct {
	Page                 int      // 0-based page index
	TextLength           int      // runes of extractable text
	TextCoverage         float64  // fraction of the page inked by text (0-1)
	ImageCount           int      // raster image objects on the page
	ImageCoverage        float64  // fraction of the page covered by rasters
	LargestImageCoverage float64  // the biggest single raster
	FullPageImage        bool     // one raster covers the page
	Garbled              bool     // the text layer decodes to nonsense
	NeedsOCR             bool     // any reason fired
	Reasons              []string
}
```

## Reasons

| Reason | Meaning |
|--------|---------|
| `scanned` | A single raster covers the page with no text behind it. |
| `no-text` | Almost no extractable text and no full-page image — a blank page, cover or divider. |
| `sparse-text` | Real text, but it inks very little of a page that also carries figures. |
| `embedded-images` | Substantial raster figures sit alongside the native text. |
| `garbled` | The text layer decodes to nonsense — broken font encodings, `(cid:NN)` runs — so the pixels are more trustworthy than the text. |

Treat `Reasons` as open-ended: route on the values you care about rather than
assuming the list is exhaustive. The thresholds are deliberately blunt — this
is a router, not a classifier, and a flagged page still parses normally.

## CLI

```bash
parserails is-complex report.pdf            # per-page JSON on stdout, verdict on stderr
parserails is-complex --compact report.pdf | jq '[.[] | select(.needs_ocr) | .page]'
parserails is-complex --pages 1-10 report.pdf
```

Exit codes make it a shell predicate:

| Code | Meaning |
|------|---------|
| `0` | every inspected page is simple |
| `2` | at least one page needs OCR |
| `1` | the document could not be inspected |

```bash
# only parse without OCR when the document is simple
parserails is-complex -q report.pdf && parserails parse report.pdf
```

Figures placed through a Form XObject — how most documents place one — are
counted too: the walk descends into form objects and maps their contents back
onto the page through the form's matrix.

> On the native `parserails_cgo` backend built **without** PDFium's
> experimental API, raster objects cannot be enumerated; image coverage then
> reports zero and only the text-based reasons fire.
