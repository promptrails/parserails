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
// are storages of their own.
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

func (msgContainer) Children(_ context.Context, data []byte) ([]Child, error) {
	f, err := openCFB(data)
	if err != nil {
		return nil, err
	}

	// Group the property streams by the attachment storage they live in.
	type attachment struct {
		name, tag string
		data      []byte
	}
	attachments := map[string]*attachment{}
	for _, s := range f.streams() {
		storage, prop, found := strings.Cut(s.Path, "/")
		if !found || !strings.HasPrefix(storage, msgAttachPrefix) {
			continue
		}
		id, kind, ok := msgProperty(prop)
		if !ok {
			continue
		}
		att := attachments[storage]
		if att == nil {
			att = &attachment{}
			attachments[storage] = att
		}
		body := f.readStream(s.Entry)
		switch id {
		case msgPropAttachData:
			att.data = body
		case msgPropAttachName:
			att.name = msgString(body, kind)
		case msgPropAttachTag:
			att.tag = msgString(body, kind)
		}
	}

	out := make([]Child, 0, len(attachments))
	for storage, att := range attachments {
		if len(att.data) == 0 {
			continue
		}
		name := att.name
		if name == "" {
			name = att.tag
		}
		if name == "" {
			name = sanitizeChildName(storage) + Sniff(att.data).Ext()
		}
		out = append(out, Child{Name: sanitizeChildName(name), Data: att.data})
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
	fields := map[string]string{}
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
			fields[id] = msgString(f.readStream(s.Entry), kind)
		}
	}

	var b strings.Builder
	for _, field := range []struct{ label, id string }{
		{"From", msgPropFrom}, {"To", msgPropTo}, {"Subject", msgPropSubject},
	} {
		if v := strings.TrimSpace(fields[field.id]); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", field.label, v)
		}
	}
	if body := strings.TrimSpace(fields[msgPropBody]); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
	}
	return strings.TrimSpace(b.String()), nil
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

func sortChildren(children []Child) {
	for i := 1; i < len(children); i++ {
		for j := i; j > 0 && children[j].Name < children[j-1].Name; j-- {
			children[j], children[j-1] = children[j-1], children[j]
		}
	}
}
