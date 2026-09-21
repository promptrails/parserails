package parserails

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

// Child is a file found inside another file. Err records a child that was
// found but could not be taken — a corrupt archive entry, or one that would
// blow the walk's remaining budget — so it is reported rather than silently
// missing.
type Child struct {
	Name string
	Data []byte
	Err  error
}

// ChildRequest carries what a container needs besides the bytes: how to open
// the containing file, and how much of the walk's budget is left.
type ChildRequest struct {
	// Name is the containing file's name, for error messages.
	Name string
	// Password opens an encrypted container (a PDF with attachments).
	Password string
	// MaxFiles and MaxBytes are what remains of the walk's budget. Zero means
	// no limit. A container must not unpack more than this: the walker's own
	// accounting happens after Children returns, which is too late to stop an
	// archive from allocating.
	MaxFiles int
	MaxBytes int64
}

// Container yields the files embedded in a document.
//
// Implement it to teach ParseRails a container it does not know — a custom
// archive, an EDI envelope, a proprietary wrapper — and register it with
// WithContainer.
type Container interface {
	// Children returns the files embedded directly in data. It does not
	// recurse: ParseRails walks the tree itself. It should stop within the
	// budget in req, reporting what it skipped as a Child with an Err.
	Children(ctx context.Context, data []byte, req ChildRequest) ([]Child, error)
}

// Node is one file in an extraction tree: the document itself, whatever text
// it carries, and the files found inside it.
type Node struct {
	Name     string    `json:"name"`
	Format   Format    `json:"format"`
	Document *Document `json:"document,omitempty"`
	// Text carries content from formats with no spatial layout of their own —
	// an e-mail body, a plain-text attachment.
	Text     string  `json:"text,omitempty"`
	Children []*Node `json:"children,omitempty"`
	// Err records why this node could not be read. It never aborts the walk:
	// one unreadable attachment should not cost you the other nine.
	Err error `json:"-"`
}

// MarshalJSON renders the node with its error as a string, so an extraction
// tree survives a round trip through JSON.
func (n *Node) MarshalJSON() ([]byte, error) {
	type alias Node // avoid recursing into this method
	out := struct {
		*alias
		Error string `json:"error,omitempty"`
	}{alias: (*alias)(n)}
	if n.Err != nil {
		out.Error = n.Err.Error()
	}
	return json.Marshal(out)
}

// AllText concatenates the text of this node and everything inside it, depth
// first, in the order the files were found.
func (n *Node) AllText() string {
	var parts []string
	n.Walk(func(node *Node) {
		switch {
		case node.Document != nil:
			if text := strings.TrimSpace(node.Document.Text()); text != "" {
				parts = append(parts, text)
			}
		case node.Text != "":
			parts = append(parts, strings.TrimSpace(node.Text))
		}
	})
	return strings.Join(parts, "\n\n")
}

// Walk calls fn for this node and every node inside it, depth first.
func (n *Node) Walk(fn func(*Node)) {
	fn(n)
	for _, c := range n.Children {
		c.Walk(fn)
	}
}

// Count reports how many files the tree holds, including this one.
func (n *Node) Count() int {
	total := 0
	n.Walk(func(*Node) { total++ })
	return total
}

// Default limits on an extraction walk. A document tree is attacker-controlled
// input: archives nest, compress enormously, and can refer to themselves.
const (
	defaultMaxDepth = 8
	defaultMaxFiles = 512
	defaultMaxBytes = 256 << 20
)

// ExtractOptions tunes an extraction walk.
type ExtractOptions struct {
	ReadOptions

	// MaxDepth is how deep to descend. Zero means 8; negative means no limit.
	MaxDepth int
	// MaxFiles caps how many files the walk will open. Zero means 512.
	MaxFiles int
	// MaxBytes caps the total size of the files opened, unpacked. Zero means
	// 256 MiB.
	MaxBytes int64
	// SkipParse collects the tree without parsing each document, which is much
	// cheaper when all you want is an inventory.
	SkipParse bool
}

func (o ExtractOptions) limits() (depth, files int, bytes int64) {
	depth, files, bytes = o.MaxDepth, o.MaxFiles, o.MaxBytes
	if depth == 0 {
		depth = defaultMaxDepth
	}
	if files == 0 {
		files = defaultMaxFiles
	}
	if bytes == 0 {
		bytes = defaultMaxBytes
	}
	return depth, files, bytes
}

// Extract reads a document and everything inside it: PDF attachments, ZIP
// entries, e-mail attachments, OLE objects embedded in office documents — and
// whatever is inside those, recursively.
//
// One unreadable file does not fail the walk; it is recorded on its own node
// as Err and the rest of the tree is still returned. The walk is bounded by
// depth, file count and total bytes, and a file that contains itself is
// visited once.
func (p *Parser) Extract(ctx context.Context, data []byte, opt ExtractOptions) (*Node, error) {
	name := opt.Name
	if name == "" {
		name = "document"
	}
	w := &walker{parser: p, opt: opt}
	w.maxDepth, w.maxFiles, w.remaining = opt.limits()
	return w.visit(ctx, name, data, 0)
}

// ExtractFile is the file counterpart of Extract.
func (p *Parser) ExtractFile(ctx context.Context, filePath string, opt ExtractOptions) (*Node, error) {
	data, _, err := readAndDetect(filePath)
	if err != nil {
		return nil, err
	}
	if opt.Name == "" {
		opt.Name = path.Base(filePath)
	}
	return p.Extract(ctx, data, opt)
}

// walker carries the budget for one extraction.
type walker struct {
	parser    *Parser
	opt       ExtractOptions
	maxDepth  int
	maxFiles  int
	files     int
	remaining int64
	seen      map[[32]byte]bool
}

func (w *walker) visit(ctx context.Context, name string, data []byte, depth int) (*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	format := Detect(name, data)
	node := &Node{Name: name, Format: format}

	w.files++
	w.remaining -= int64(len(data))
	if w.maxFiles > 0 && w.files > w.maxFiles {
		node.Err = fmt.Errorf("parserails: extraction stopped: more than %d files", w.maxFiles)
		return node, nil
	}
	if w.remaining < 0 {
		node.Err = fmt.Errorf("parserails: extraction stopped: unpacked size limit reached")
		return node, nil
	}
	if fingerprint := sha256.Sum256(data); w.markSeen(fingerprint) {
		node.Err = fmt.Errorf("parserails: already extracted this file")
		return node, nil
	}

	w.read(ctx, node, data)
	if w.maxDepth >= 0 && depth >= w.maxDepth {
		return node, nil
	}

	container, ok := w.parser.containers[format]
	if !ok {
		return node, nil
	}
	children, err := w.children(ctx, container, node, data)
	if err != nil {
		node.Err = err
		return node, nil
	}
	for _, child := range children {
		if child.Err != nil {
			// Found but not taken: keep it in the tree with its reason.
			node.Children = append(node.Children, &Node{Name: child.Name, Err: child.Err})
			w.files++
			continue
		}
		sub, err := w.visit(ctx, child.Name, child.Data, depth+1)
		if err != nil {
			return nil, err // only context cancellation gets here
		}
		node.Children = append(node.Children, sub)
	}
	return node, nil
}

// children enumerates one node's embedded files, under the same timeout as
// parsing: a container that reopens the document (PDF attachments) makes
// PDFium calls of its own, which no context can interrupt.
func (w *walker) children(ctx context.Context, container Container, node *Node, data []byte) ([]Child, error) {
	req := ChildRequest{
		Name:     node.Name,
		Password: w.opt.Password,
		MaxBytes: w.remaining,
	}
	if w.maxFiles > 0 {
		req.MaxFiles = max(w.maxFiles-w.files, 0)
	}
	return bounded(ctx, w.parser, func(ctx context.Context) ([]Child, error) {
		return container.Children(ctx, data, req)
	})
}

// read fills in a node's own content, recording rather than returning errors.
func (w *walker) read(ctx context.Context, node *Node, data []byte) {
	if w.opt.SkipParse {
		return
	}
	switch {
	case node.Format == FormatText:
		node.Text = string(data)
	case node.Format == FormatEML:
		text, err := emlText(data)
		if err != nil {
			node.Err = err
			return
		}
		node.Text = text
	case node.Format == FormatMSG:
		text, err := msgText(data)
		if err != nil {
			node.Err = err
			return
		}
		node.Text = text
	case node.Format.IsOOXML() && w.parser.nativeOffice:
		office, err := ReadOfficeDocument(data, node.Format)
		if err != nil {
			node.Err = err
			return
		}
		node.Text = office.Text()
	case node.Format == FormatPDF, node.Format.IsOffice(),
		node.Format.IsImage() && w.parser.hasOCR():
		opt := w.opt.ReadOptions
		opt.Name = node.Name
		doc, err := w.parser.ParseData(ctx, data, opt)
		if err == nil {
			node.Document = doc
			return
		}
		// A missing LibreOffice is a missing dependency, not a broken
		// document: an OOXML package can still be read without it.
		if errors.Is(err, ErrNoLibreOffice) && node.Format.IsOOXML() {
			if office, nativeErr := ReadOfficeDocument(data, node.Format); nativeErr == nil {
				node.Text = office.Text()
				return
			}
		}
		node.Err = err
	}
}

func (w *walker) markSeen(fingerprint [32]byte) bool {
	if w.seen == nil {
		w.seen = make(map[[32]byte]bool)
	}
	if w.seen[fingerprint] {
		return true
	}
	w.seen[fingerprint] = true
	return false
}

// defaultContainers is the set of containers a new Parser knows.
func defaultContainers(p *Parser) map[Format]Container {
	zip := zipContainer{}
	ooxml := ooxmlContainer{}
	ole := oleContainer{}
	return map[Format]Container{
		FormatPDF:  pdfContainer{parser: p},
		FormatZIP:  zip,
		FormatEML:  emlContainer{},
		FormatDOCX: ooxml,
		FormatXLSX: ooxml,
		FormatPPTX: ooxml,
		FormatOLE:  ole,
		FormatDOC:  ole,
		FormatXLS:  ole,
		FormatPPT:  ole,
		FormatMSG:  msgContainer{},
	}
}
