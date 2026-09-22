package parserails

import (
	"context"
	"fmt"
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

func (msgContainer) Children(_ context.Context, data []byte, req ChildRequest) ([]Child, error) {
	f, err := openCFB(data)
	if err != nil {
		return nil, err
	}

	type attachment struct {
		name, tag string
		data      []byte
		err       error
		nested    map[string]string // a message attached to a message
	}
	budget := newChildBudget(req)
	attachments := map[string]*attachment{}
	for _, s := range f.streams() {
		storage, prop, found := strings.Cut(s.Path, "/")
		if !found || !strings.HasPrefix(storage, msgAttachPrefix) {
			continue
		}
		att := attachments[storage]
		if att == nil {
			att = &attachment{nested: map[string]string{}}
			attachments[storage] = att
		}

		// A property of the attachment itself, or one belonging to a message
		// attached to it? Everything below the first "/" is the nested
		// message's own property set, and assigning those to att.data — as a
		// plain suffix match would — leaves the last nested stream standing
		// in for the attachment.
		if inner, deeper := strings.CutPrefix(prop, msgPropPrefix+msgPropAttachData+"000D/"); deeper {
			id, kind, ok := msgProperty(inner)
			if !ok {
				continue
			}
			// A forwarded message is an attachment like any other: its
			// property streams are read against the same budget.
			limit, room := budget.room(maxChildBytes)
			if !room || !budget.fits(s.Entry.Size, limit) {
				budget.refuse()
				att.err = fmt.Errorf(
					"parserails: embedded message is larger than the remaining %d byte budget", limit)
				continue
			}
			text := msgString(f.readStream(s.Entry), kind)
			budget.spend(int64(len(text)))
			att.nested[id] = text
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
			limit, ok := budget.room(maxChildBytes)
			if !ok {
				att.err = budget.exceeded()
				continue
			}
			if !budget.fits(s.Entry.Size, limit) {
				// Recorded, not dropped: an attachment that silently
				// disappears makes a partial extraction look complete.
				budget.refuse()
				att.err = fmt.Errorf(
					"parserails: attachment is larger than the remaining %d byte budget", limit)
				continue
			}
			att.data = f.readStream(s.Entry)
			budget.spend(int64(len(att.data)))
		case msgPropAttachName:
			att.name = msgString(f.readStream(s.Entry), kind)
		case msgPropAttachTag:
			att.tag = msgString(f.readStream(s.Entry), kind)
		}
	}

	out := make([]Child, 0, len(attachments))
	for storage, att := range attachments {
		name := firstNonEmpty(att.name, att.tag)
		switch {
		case att.err != nil:
			if name == "" {
				name = sanitizeChildName(storage)
			}
			out = append(out, Child{Name: name, Err: att.err})
		case len(att.data) > 0:
			if name == "" {
				name = sanitizeChildName(storage) + Sniff(att.data).Ext()
			}
			out = append(out, Child{Name: sanitizeChildName(name), Data: att.data})
		case att.err == nil && len(att.nested) > 0:
			// A forwarded message. Its parts cannot be reassembled into a
			// .msg, so its text is carried out instead of being lost.
			if name == "" {
				name = strings.TrimPrefix(sanitizeChildName(storage), msgAttachPrefix)
			}
			out = append(out, Child{
				Name: strings.TrimSuffix(name, ".msg") + ".txt",
				Data: []byte(msgTextFrom(att.nested)),
			})
		}
	}
	sortChildren(out)
	return out, nil
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
