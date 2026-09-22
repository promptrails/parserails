package parserails

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
)

// maxMailParts bounds a malformed or hostile message's part tree.
const maxMailParts = 256

// emlText renders an e-mail's headers and body as text. Attachments are not
// included: they come back from the container as children of their own.
func emlText(data []byte) (string, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("parserails: read e-mail: %w", err)
	}

	var b strings.Builder
	dec := new(mime.WordDecoder)
	for _, name := range []string{"Date", "From", "To", "Cc", "Subject"} {
		value := msg.Header.Get(name)
		if value == "" {
			continue
		}
		if decoded, err := dec.DecodeHeader(value); err == nil {
			value = decoded
		}
		fmt.Fprintf(&b, "%s: %s\n", name, value)
	}

	body, _, err := walkMailPart(
		msg.Header.Get("Content-Type"),
		msg.Header.Get("Content-Transfer-Encoding"),
		msg.Body,
		newChildBudget(ChildRequest{}),
	)
	if err != nil {
		return strings.TrimSpace(b.String()), err
	}
	if body = strings.TrimSpace(body); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
	}
	return strings.TrimSpace(b.String()), nil
}

// walkMailPart reads one MIME part, returning its readable text and the
// attachments below it. Nested multiparts — the usual mixed/alternative/related
// nesting real mail clients emit — are walked recursively.
func walkMailPart(contentType, encoding string, body io.Reader, budget *childBudget) (string, []Child, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || contentType == "" {
		mediaType = "text/plain" // a message with no Content-Type is plain text
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", nil, nil
		}
		return walkMultipart(multipart.NewReader(body, boundary), mediaType == "multipart/alternative", budget)
	}

	// Text is the message, not an attachment, so it is read against the
	// per-file cap; anything else is a child and is charged to the budget.
	if mediaType == "text/plain" || mediaType == "text/html" {
		raw, err := io.ReadAll(io.LimitReader(body, maxChildBytes))
		if err != nil {
			return "", nil, fmt.Errorf("parserails: read mail part: %w", err)
		}
		decoded := decodeTransfer(raw, encoding)
		if mediaType == "text/html" {
			return stripHTML(string(decoded)), nil, nil
		}
		return string(decoded), nil, nil
	}

	// A part with no file name and no disposition is still a file: an inline
	// application/octet-stream is an attachment in everything but its
	// headers, and skipping the budget here left the largest parts unmetered.
	child := readMailChild(body, encoding, "part", budget)
	return "", []Child{child}, nil
}

// walkMultipart reads the parts of a multipart body. In an "alternative" body
// the parts are the same content in different forms, so the best single one
// wins instead of all of them being concatenated.
func walkMultipart(mr *multipart.Reader, alternative bool, budget *childBudget) (string, []Child, error) {
	var (
		texts       []string
		attachments []Child
		best        string
	)
	for i := 0; i < maxMailParts; i++ {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break // a truncated message still gives up what it has
		}
		disposition := part.Header.Get("Content-Disposition")
		contentType := part.Header.Get("Content-Type")

		if isAttachment(disposition, part.FileName()) {
			name := mimeFilename(disposition, contentType, len(attachments)+1)
			child := readMailChild(part, part.Header.Get("Content-Transfer-Encoding"), name, budget)
			attachments = append(attachments, child)
			if child.Err != nil && errors.Is(child.Err, errBudgetSpent) {
				break
			}
			continue
		}

		text, nested, err := walkMailPart(contentType, part.Header.Get("Content-Transfer-Encoding"), part, budget)
		if err != nil {
			continue
		}
		attachments = append(attachments, nested...)
		if text = strings.TrimSpace(text); text == "" {
			continue
		}
		if alternative {
			// Plain text beats HTML converted to text; later parts of an
			// alternative body are richer, so only take one.
			if best == "" || strings.HasPrefix(contentType, "text/plain") {
				best = text
			}
			continue
		}
		texts = append(texts, text)
	}
	if alternative {
		return best, attachments, nil
	}
	return strings.Join(texts, "\n\n"), attachments, nil
}

// errBudgetSpent marks the end of the walk's budget, as opposed to one part
// being too large for what is left of it.
var errBudgetSpent = errors.New("parserails: extraction stopped: the walk's remaining budget is spent")

// readMailChild reads one part as an embedded file, within the budget.
//
// The budget counts unpacked bytes, so the transfer encoding is undone before
// the size is judged: base64 is a third larger than what it carries, and
// measuring the encoded form refused attachments that fit.
func readMailChild(r io.Reader, encoding, name string, budget *childBudget) Child {
	limit, ok := budget.room(maxChildBytes)
	if !ok {
		return Child{Name: name, Err: errBudgetSpent}
	}

	// Decoded while reading, and limited on the decoded side: guessing an
	// encoded size cannot work. Quoted-printable triples a byte in the worst
	// case, base64 adds a line break every 76 characters, and a guess that
	// comes up short truncates the attachment without saying so.
	data, err := io.ReadAll(io.LimitReader(transferReader(r, encoding), limit+1))
	if err != nil {
		budget.refuse()
		return Child{Name: name, Err: fmt.Errorf("parserails: read mail part: %w", err)}
	}
	if int64(len(data)) > limit {
		budget.refuse()
		return Child{Name: name,
			Err: fmt.Errorf("parserails: attachment is larger than the remaining %d byte budget", limit)}
	}
	budget.spend(int64(len(data)))
	return Child{Name: name, Data: data}
}

func isAttachment(disposition, filename string) bool {
	if filename != "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(disposition)
	return err == nil && mediaType == "attachment"
}

// transferReader undoes a part's transfer encoding as it is read, so a limit
// can be applied to the bytes the part actually carries.
func transferReader(r io.Reader, encoding string) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		// The decoder skips the line breaks base64 bodies are wrapped at.
		return base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	default:
		return r
	}
}

func decodeTransfer(data []byte, encoding string) []byte {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		out, err := base64.StdEncoding.DecodeString(stripSpace(string(data)))
		if err != nil {
			return data
		}
		return out
	case "quoted-printable":
		out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(data)))
		if err != nil {
			return data
		}
		return out
	default:
		return data
	}
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		}
		return r
	}, s)
}

// stripHTML reduces an HTML body to its text. It is deliberately crude: this
// is an e-mail body destined for an index or an LLM, not a rendering engine.
func stripHTML(s string) string {
	var (
		b    strings.Builder
		skip bool
	)
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '<':
			skip = true
			lower := strings.ToLower(s[i:min(i+7, len(s))])
			if strings.HasPrefix(lower, "<br") || strings.HasPrefix(lower, "</p") ||
				strings.HasPrefix(lower, "</div") || strings.HasPrefix(lower, "</tr") {
				b.WriteByte('\n')
			}
		case s[i] == '>':
			skip = false
		case !skip:
			b.WriteByte(s[i])
		}
	}
	text := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`).
		Replace(b.String())

	lines := strings.Split(text, "\n")
	out := lines[:0]
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
