# Containers: files inside files

Real corpora are not flat. An invoice arrives as a PDF attached to an e-mail
attached to a ZIP; a contract has a spreadsheet pasted into it; an e-invoice
carries its XML inside the PDF. `Extract` walks all of it.

```go
node, err := p.ExtractFile(ctx, "bundle.zip", parserails.ExtractOptions{})
fmt.Println(node.AllText()) // everything, depth first
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
its own node and the other nine still come back — which is the difference
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

Every reader enforces the budget — ZIP entries, PDF attachments, MIME parts,
OLE and Outlook streams — and an exhausted budget stops the walk rather than
being passed on as "no limit". The byte budget is spent **as each entry is
decompressed**, not counted after the fact — an archive of a thousand entries that each expand to 64 MiB would
otherwise allocate 64 GiB before anything checked the total. An entry that
would exceed what is left is reported as a node with an `Err`, so it is
visible rather than silently missing.

`SkipParse: true` collects the tree without parsing anything, which is much
cheaper when you only want an inventory.

## Teaching it a new container

```go
type tarContainer struct{}

func (tarContainer) Children(
	ctx context.Context, data []byte, req parserails.ChildRequest,
) ([]parserails.Child, error) {
	// One level only: ParseRails walks the tree itself. Stay inside
	// req.MaxFiles and req.MaxBytes — what is left of the walk's budget —
	// and report what you skipped as a Child with an Err. req.Password is
	// the password this read was opened with, for containers that need it.
}

p, _ := parserails.New(parserails.WithContainer(parserails.FormatZIP, tarContainer{}))
```

`WithContainer` also replaces a built-in one — or, with a container that
returns nothing, switches unpacking off for a format.

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
