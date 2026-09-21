package parserails

import (
	"strings"
)

// Markdown renders the document as Markdown with the default options.
func (d *Document) Markdown() string { return d.MarkdownWith(BlockOptions{}) }

// MarkdownWith renders the document's reconstructed structure as Markdown:
// headings, paragraphs, lists, tables and image placeholders, in reading
// order.
//
// This is the shape LLM and RAG pipelines want — structure a plain-text dump
// throws away — and it is produced from geometry alone: no model, no network,
// no layout engine. Complex documents render imperfectly; the
// [Blocks] decomposition it is built from is available as data when you need
// to handle those cases yourself.
func (d *Document) MarkdownWith(opt BlockOptions) string {
	return renderMarkdown(d.BlocksWith(opt))
}

// renderMarkdown writes a block sequence as Markdown. It is shared by the
// spatial reconstruction and by documents read natively out of an office
// package, which produce the same blocks by other means.
func renderMarkdown(blocks []Block) string {
	var (
		b    strings.Builder
		last BlockKind
	)
	for _, block := range blocks {
		text := strings.TrimSpace(block.Text)
		if text == "" && block.Kind != BlockTable && block.Kind != BlockFigure {
			continue
		}
		// Consecutive list items form one list; everything else is separated
		// by a blank line.
		if b.Len() > 0 {
			if block.Kind == BlockListItem && last == BlockListItem {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}

		switch block.Kind {
		case BlockHeading:
			b.WriteString(strings.Repeat("#", clampLevel(block.Level)))
			b.WriteByte(' ')
			b.WriteString(text)
		case BlockListItem:
			if block.Ordered {
				b.WriteString(block.Marker + " " + text)
			} else {
				b.WriteString("- " + text)
			}
		case BlockTable:
			b.WriteString(markdownTable(block))
		case BlockFigure:
			b.WriteString("![](" + block.ID + ".png)")
		default:
			b.WriteString(text)
		}
		last = block.Kind
	}
	b.WriteString("\n")
	return b.String()
}

func clampLevel(level int) int {
	switch {
	case level < 1:
		return 1
	case level > 6:
		return 6
	}
	return level
}

// markdownTable renders a GitHub-flavoured pipe table. A table without a
// detected header row still needs one to be valid Markdown, so it gets an
// empty one.
func markdownTable(block Block) string {
	header := block.Header
	rows := block.Rows
	width := len(header)
	for _, row := range rows {
		width = max(width, len(row))
	}
	if width == 0 {
		return ""
	}
	if len(header) == 0 {
		header = make([]Cell, width)
	}

	var b strings.Builder
	writeRow(&b, header, width)
	b.WriteString("|")
	for i := 0; i < width; i++ {
		b.WriteString(" --- |")
	}
	for _, row := range rows {
		b.WriteString("\n")
		writeRow(&b, row, width)
	}
	return b.String()
}

func writeRow(b *strings.Builder, row []Cell, width int) {
	b.WriteString("|")
	for i := 0; i < width; i++ {
		text := ""
		if i < len(row) {
			text = escapePipes(strings.TrimSpace(row[i].Text))
		}
		b.WriteString(" " + text + " |")
	}
	b.WriteString("\n")
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
