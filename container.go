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

// childBudget is what a container may still unpack, tracked so that "nothing
// left" stays distinguishable from "no limit set" — a spent budget read as
// unlimited is the same bug as having no budget at all.
type childBudget struct {
	limitFiles, limitBytes bool
	files                  int
	bytes                  int64
}

func newChildBudget(req ChildRequest) *childBudget {
	b := &childBudget{}
	if req.MaxFiles > 0 {
		b.limitFiles, b.files = true, req.MaxFiles
	}
	if req.MaxBytes > 0 {
		b.limitBytes, b.bytes = true, req.MaxBytes
	}
	return b
}

// room reports how many bytes may still be read, capped at most, and whether
// there is room for another file at all.
func (b *childBudget) room(most int64) (int64, bool) {
	if b.limitFiles && b.files <= 0 {
		return 0, false
	}
	if !b.limitBytes {
		return most, true
	}
	if b.bytes <= 0 {
		return 0, false
	}
	return min(b.bytes, most), true
}

// spend records a child that was taken.
func (b *childBudget) spend(size int64) {
	b.files--
	b.bytes -= size
}

// refuse records a child that was found and rejected. It costs a file slot
// too: otherwise a small MaxFiles bounds what is kept but not what is
// attempted, and an archive of ten thousand oversized entries is still ten
// thousand attempts.
func (b *childBudget) refuse() { b.files-- }

// fits reports whether a size declared by a document fits what is left of the
// budget. Declared sizes are unsigned and untrusted; the budget never is.
func (b *childBudget) fits(size uint64, limit int64) bool {
	// #nosec G115 -- limit comes from room, which returns a positive number
	// or reports that there is no room at all.
	return limit > 0 && size <= uint64(limit)
}

// exceeded is the error a container reports for a child it found but did not
// take, so the file stays visible in the tree.
func (b *childBudget) exceeded() error {
	return fmt.Errorf("parserails: extraction stopped: the walk's remaining budget is spent")
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
	return w.visit(ctx, name, data, 0, false)
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

// visit reads one file and descends into it. charged says whether this node
// was already taken out of the budget — both its bytes and its file slot —
// when its parent unpacked it. That reservation happens before the walk
// descends, so a nested archive cannot be handed a fresh budget at every
// level while its siblings are still in memory.
func (w *walker) visit(ctx context.Context, name string, data []byte, depth int, charged bool) (*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	format := Detect(name, data)
	node := &Node{Name: name, Format: format}

	if !charged {
		w.files++
		w.remaining -= int64(len(data))
	}
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
	// Reserve what the container produced before descending into any of it:
	// the bytes are already held, and counting a level only when the walk
	// reaches it lets every level believe the whole budget is still free —
	// for its siblings' file slots as much as for their bytes.
	w.files += len(children)
	for _, child := range children {
		w.remaining -= int64(len(child.Data))
	}
	for _, child := range children {
		if child.Err != nil {
			// Found but not taken: keep it in the tree with its reason.
			node.Children = append(node.Children, &Node{Name: child.Name, Err: child.Err})
			continue
		}
		sub, err := w.visit(ctx, child.Name, child.Data, depth+1, true)
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
	}
	// Zero means "no limit" in the contract, so an exhausted budget has to
	// stop the walk here rather than be passed on as no limit at all. It is
	// reported as an unread child, not as this node's error: this file was
	// read, it is its contents that were not.
	if w.remaining <= 0 {
		return []Child{{Name: node.Name + " (contents)",
			Err: fmt.Errorf("parserails: extraction stopped: unpacked size limit reached")}}, nil
	}
	req.MaxBytes = w.remaining
	if w.maxFiles > 0 {
		if w.files >= w.maxFiles {
			return []Child{{Name: node.Name + " (contents)",
				Err: fmt.Errorf("parserails: extraction stopped: more than %d files", w.maxFiles)}}, nil
		}
		req.MaxFiles = w.maxFiles - w.files
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
		text, err := w.parser.nativeOfficeText(ctx, data, node.Format)
		if err != nil {
			node.Err = err
			return
		}
		node.Text = text
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
			if text, nativeErr := w.parser.nativeOfficeText(ctx, data, node.Format); nativeErr == nil {
				node.Text = text
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
