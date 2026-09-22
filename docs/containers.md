# Containers: files inside files

Real corpora are not flat. An invoice arrives as a PDF attached to an e-mail
attached to a ZIP; a contract has a spreadsheet pasted into it; an e-invoice
carries its XML inside the PDF. `Extract` walks all of it.

```go
node, err := p.ExtractFile(ctx, "bundle.zip", parserails.ExtractOptions{})
if err != nil {
	return err
}
node.Walk(func(n *parserails.Node) {
	if n.Err != nil {
		log.Printf("%s: %v", n.Name, n.Err)
	}
})
fmt.Println(node.AllText()) // recovered text, depth first
```

```bash
parserails extract bundle.zip          # a tree view
parserails extract --format text mail.eml
parserails extract --format json --list archive.zip  # inventory only
```

```
bundle.zip  [zip]
  invoice.pdf  [pdf]  1 page(s)
  notes.txt  [txt]  10 char(s)
```

`Extract` returns text and an inventory tree; it does not write the original
attachment bytes to disk. For an end-to-end example, see the [Usage Guide](usage-guide.md).

## What gets unpacked

| Format | What comes out |
|--------|----------------|
| PDF | embedded file attachments (e-invoice XML, appendices) |
| ZIP | every entry |
| DOCX / XLSX / PPTX | the objects under `/embeddings/` — not the package's own XML |
| EML | MIME attachments; the body becomes the node's text |
| MSG | Outlook attachments; subject, sender and body become the node's text |
| DOC / XLS / PPT / OLE | `Ole10Native` package payloads, plus any stream that is a document in its own right |

Legacy formats are read through a real **Compound File Binary** parser — the
FAT, the directory tree and the mini-stream — not by scanning the bytes for a
magic number. Scanning finds whatever fragment happens to look like a file and
misses everything stored below the mini-stream cutoff or fragmented across
sectors.

## The tree

```go
type Node struct {
	Name     string
	Format   Format
	Document *Document // parsed, for formats with a spatial layout
	Text     string    // for formats without one: e-mail bodies, plain text
	Children []*Node
	Err      error     // why this node could not be read
}
```

`node.Walk(fn)`, `node.AllText()` and `node.Count()` cover the usual traversals,
and the tree marshals to JSON with each node's error included.

One unreadable file never fails the walk. A corrupt attachment is recorded on
its own node and the other nine still come back — as is a file that was found
but not taken, so a partial extraction never looks complete — which is the difference
between a batch that finishes and a batch that stops on document 4,000.

## Limits

A document tree is attacker-controlled input: archives nest, compress
enormously, and can contain themselves.

| Option | Default | Guards against |
|--------|---------|----------------|
| `MaxDepth` | 8 | endlessly nested containers |
| `MaxFiles` | 512 | archives with a million entries |
| `MaxBytes` | 256 MiB | zip bombs |
| — | always on | a file that contains itself: identical content is visited once, by hash |

> One gap worth knowing: PDFium hands over an embedded attachment in one
> piece, with no way to ask its size first, so a PDF attachment is measured
> after it has been materialized. It is reported and dropped rather than
> parsed, but a single enormous attachment is held in memory once before that
> happens. The byte budget is not a process memory cap; see [Limits & Timeouts](limits.md).

Every reader enforces the budget — ZIP entries, PDF attachments, MIME parts,
OLE and Outlook streams — and an exhausted budget stops the walk rather than
being passed on as "no limit". A refused file costs a file slot too, so a
small `MaxFiles` bounds what is *attempted*, not only what is kept. What a
container unpacks is charged before the walk descends into it, so a nested
archive cannot be handed the whole budget again at every level. The budget
counts **unpacked** bytes: a base64 attachment is measured after decoding.

The byte budget is spent **as each entry is decompressed**, not counted after
the fact — an archive of a thousand entries that each expand to 64 MiB would
otherwise allocate 64 GiB before anything checked the total. An entry that
would exceed what is left is reported as a node with an `Err`, so it is
visible rather than silently missing.

The root consumes a file slot and its byte length. Container bytes and their
unpacked children both count. A node at `MaxDepth` is read but its children are
not enumerated; reaching that depth does not add an error marker. Stop notices
can make `Node.Count()` larger than `MaxFiles`. Identical bytes anywhere in the
walk are parsed only once, even when their names differ.

`SkipParse: true` collects the tree without parsing document text. Containers
are still opened and decompressed to find descendants; it is not a metadata-only
archive scan. See [Limits & Timeouts](limits.md) for defaults, deadline scope,
and partial-result semantics.

## Email and Outlook details

EML body text is recorded on the parent node and attachments become children.
MIME base64 and quoted-printable attachments are budgeted after decoding.
Multipart bodies, including forwarded `message/rfc822` attachments, are walked
recursively. An attachment that exceeds the budget is reported rather than
returned as truncated data.

MSG headers/body become the parent text. Binary attachments retain their names;
a message attached as an embedded Outlook storage becomes a `.txt` child with
its rendered headers and body. It is not reconstructed into another MSG file.
One embedded message consumes one file slot, and its emitted text is charged
once, including headers and separators. UTF-16 property storage can be larger
than that output, so encoded reads are bounded separately.

MSG attachments are processed in storage-name order with one shared budget.
When the quota is spent, later attachments are not read; a stop notice is
included. Actual failed attempts still consume a file slot. The final children
are sorted by display name, which may differ from their processing order.

## Handling errors

The returned error covers call-level failures such as an already-canceled
context. Individual parse, container and quota failures usually live on
`Node.Err`, including errors on the root. Walk the tree before treating it as
complete. `AllText()` omits the error report, and CLI extraction may exit zero
with node errors. Images without OCR and unsupported leaf formats can be present
as inventory nodes without text or an error.

JSON includes each node's error under `error`; rejected children may have
`format: "unknown"` because their content was never inspected.

## Replacing a container reader

```go
type skipAttachments struct{}

func (skipAttachments) Children(
	ctx context.Context, data []byte, req parserails.ChildRequest,
) ([]parserails.Child, error) {
	return nil, nil // example: keep the parent, skip all embedded files
}

p, err := parserails.New(
	parserails.WithContainer(parserails.FormatPDF, skipAttachments{}),
)
```

`WithContainer` registers a reader for a detected format, replacing its
built-in reader. The example disables unpacking PDF attachments. This registry
does not add new magic-byte detection or filename extensions.

A reader returns one level only; ParseRails handles recursion. Enforce
`req.MaxFiles` and `req.MaxBytes` **before** reading children, since the walker
can only account for allocations after `Children` returns. Zero means no limit
in this request type. Honor `ctx` and `req.Password` where applicable, and
report an attempted but refused child with `Child.Err`. Stop after exhausting
the budget instead of returning one ordinary error per unattempted entry;
ordinary child errors consume global file slots.

```go
type Container interface {
	Children(ctx context.Context, data []byte, req ChildRequest) ([]Child, error)
}

type Child struct {
	Name string
	Data []byte
	Err  error // found but not taken: corrupt, or over the remaining budget
}

type ChildRequest struct {
	Name     string // the containing file's name
	Password string // for an encrypted container
	MaxFiles int    // what is left of the walk's budget; 0 means no limit
	MaxBytes int64
}
```
