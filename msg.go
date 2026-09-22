package parserails

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"
)

// Outlook stores a .msg as a Compound File whose streams are MAPI properties:
// "__substg1.0_<id><type>", where id identifies the property and type its
// encoding (001F = UTF-16 text, 001E = 8-bit text, 0102 = binary). Attachments
// are storages of their own, and an attachment that is itself a message is a
// storage inside that one.
const (
	msgPropPrefix     = "__substg1.0_"
	msgAttachPrefix   = "__attach_version1.0_"
	msgPropSubject    = "0037"
	msgPropBody       = "1000"
	msgPropFrom       = "0042"
	msgPropTo         = "0E04"
	msgPropAttachData = "3701"
	msgPropAttachName = "3707" // long file name
	msgPropAttachTag  = "3704" // 8.3 file name, the fallback
)

// msgContainer yields an Outlook message's attachments.
type msgContainer struct{}

// msgPropertyStream describes a property without reading its payload.
type msgPropertyStream struct {
	entry cfbEntry
	kind  string
}

func (msgContainer) Children(ctx context.Context, data []byte, req ChildRequest) ([]Child, error) {
	f, err := openCFB(data)
	if err != nil {
		return nil, err
	}

	type attachment struct {
		name, tag msgPropertyStream
		data      *cfbEntry
		nested    map[string]msgPropertyStream
	}
	attachments := map[string]*attachment{}
	var order []string
	for _, s := range f.streams() {
		storage, prop, found := strings.Cut(s.Path, "/")
		if !found || !strings.HasPrefix(storage, msgAttachPrefix) {
			continue
		}
		att := attachments[storage]
		if att == nil {
			att = &attachment{nested: map[string]msgPropertyStream{}}
			attachments[storage] = att
			order = append(order, storage)
		}
		if inner, deeper := strings.CutPrefix(prop, msgPropPrefix+msgPropAttachData+"000D/"); deeper {
			id, kind, ok := msgProperty(inner)
			if !ok || strings.Contains(inner, "/") || (kind != "001F" && kind != "001E") {
				continue
			}
			switch id {
			case msgPropSubject, msgPropBody, msgPropFrom, msgPropTo:
				att.nested[id] = msgPropertyStream{s.Entry, kind}
			}
			continue
		}
		if strings.Contains(prop, "/") {
			continue
		}
		id, kind, ok := msgProperty(prop)
		if !ok {
			continue
		}
		switch id {
		case msgPropAttachData:
			att.data = &s.Entry
		case msgPropAttachName:
			att.name = msgPropertyStream{s.Entry, kind}
		case msgPropAttachTag:
			att.tag = msgPropertyStream{s.Entry, kind}
		}
	}

	// Reserve and finish one attachment before opening the next. Iterating
	// the map here would make a limited extraction choose random siblings.
	slices.Sort(order)
	budget := newChildBudget(req)
	var out []Child
	for _, storage := range order {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		att := attachments[storage]
		if att.data == nil && len(att.nested) == 0 {
			continue
		}
		limit, room := budget.room(maxChildBytes)
		if !room {
			out = append(out, Child{Name: sanitizeChildName(storage), Err: budget.exceeded()})
			break
		}
		budget.takeFile()

		// Names are metadata, not child content. Bound their reads separately
		// so a short payload can still retain its full attachment name.
		name, _ := readMSGProperty(f, att.name, 4<<10)
		tag, _ := readMSGProperty(f, att.tag, 4<<10)
		child := Child{Name: sanitizeChildName(firstNonEmpty(name, tag, storage))}
		if att.data != nil {
			if !budget.fits(att.data.Size, limit) {
				child.Err = fmt.Errorf("parserails: attachment is larger than the remaining %d byte budget", limit)
			} else {
				child.Data = f.readStream(*att.data)
				if name == "" && tag == "" {
					child.Name += Sniff(child.Data).Ext()
				}
			}
		} else {
			if name == "" && tag == "" {
				child.Name = strings.TrimPrefix(child.Name, msgAttachPrefix)
			}
			child.Name = strings.TrimSuffix(child.Name, ".msg") + ".txt"
			props := map[string]string{}
			// Only these four properties contribute to the emitted message.
			// Check the rendered size as they accumulate, including headers.
			for _, id := range []string{msgPropFrom, msgPropTo, msgPropSubject, msgPropBody} {
				prop, ok := att.nested[id]
				if !ok {
					continue
				}
				text, err := readMSGProperty(f, prop, limit)
				if err != nil {
					child.Err = err
					break
				}
				props[id] = text
				if int64(len(msgTextFrom(props))) > limit {
					child.Err = fmt.Errorf("parserails: embedded message is larger than the remaining %d byte budget", limit)
					break
				}
			}
			if child.Err == nil {
				child.Data = []byte(msgTextFrom(props))
			}
		}
		if child.Err == nil {
			budget.takeBytes(int64(len(child.Data)))
		}
		out = append(out, child)
	}
	sortChildren(out)
	return out, nil
}

// readMSGProperty bounds the encoded read separately from the decoded text.
// UTF-16 can use two bytes for each output byte; allow its optional string
// terminator too. The absolute per-stream cap still applies to raw reads.
func readMSGProperty(f *cfbFile, prop msgPropertyStream, limit int64) (string, error) {
	rawLimit := limit + 1
	if prop.kind == "001F" {
		rawLimit = 2 * (limit + 1)
	}
	rawLimit = min(rawLimit, maxChildBytes)
	// #nosec G115 -- callers pass a positive limit capped at maxChildBytes.
	if prop.entry.Size > uint64(rawLimit) {
		return "", fmt.Errorf("parserails: message property exceeds the %d byte read limit", rawLimit)
	}
	text := msgString(f.readStream(prop.entry), prop.kind)
	if int64(len(text)) > limit {
		return "", fmt.Errorf("parserails: decoded message property exceeds the remaining %d byte budget", limit)
	}
	return text, nil
}

// msgText renders an Outlook message's headers and body as text.
func msgText(data []byte) (string, error) {
	f, err := openCFB(data)
	if err != nil {
		return "", err
	}
	props := map[string]string{}
	for _, s := range f.streams() {
		if strings.Contains(s.Path, "/") {
			continue // belongs to an attachment, not to the message
		}
		id, kind, ok := msgProperty(s.Path)
		if !ok {
			continue
		}
		switch id {
		case msgPropSubject, msgPropBody, msgPropFrom, msgPropTo:
			props[id] = msgString(f.readStream(s.Entry), kind)
		}
	}
	return msgTextFrom(props), nil
}

// msgTextFrom renders a property set as headers plus body.
func msgTextFrom(props map[string]string) string {
	var b strings.Builder
	for _, field := range []struct{ label, id string }{
		{"From", msgPropFrom}, {"To", msgPropTo}, {"Subject", msgPropSubject},
	} {
		if v := strings.TrimSpace(props[field.id]); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", field.label, v)
		}
	}
	if body := strings.TrimSpace(props[msgPropBody]); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
	}
	return strings.TrimSpace(b.String())
}

// msgProperty splits a property stream name into its property id and type.
func msgProperty(name string) (id, kind string, ok bool) {
	rest, found := strings.CutPrefix(name, msgPropPrefix)
	if !found || len(rest) < 8 {
		return "", "", false
	}
	return strings.ToUpper(rest[:4]), strings.ToUpper(rest[4:8]), true
}

// msgString decodes a property value according to its MAPI type.
func msgString(data []byte, kind string) string {
	if kind == "001F" { // PT_UNICODE, UTF-16LE
		units := make([]uint16, 0, len(data)/2)
		for i := 0; i+1 < len(data); i += 2 {
			units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
		}
		return strings.TrimRight(string(utf16.Decode(units)), "\x00")
	}
	return strings.TrimRight(string(data), "\x00")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func sortChildren(children []Child) {
	for i := 1; i < len(children); i++ {
		for j := i; j > 0 && children[j].Name < children[j-1].Name; j-- {
			children[j], children[j-1] = children[j-1], children[j]
		}
	}
}
