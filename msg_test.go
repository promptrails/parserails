package parserails

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func embeddedMSGAttachment(index int, name string, props ...cfbTestEntry) cfbTestEntry {
	return cfbTestEntry{
		Name: fmt.Sprintf("__attach_version1.0_#%08d", index),
		Children: []cfbTestEntry{
			{Name: "__substg1.0_3707001F", Data: utf16Bytes(name)},
			{Name: "__substg1.0_3701000D", Children: props},
		},
	}
}

func TestEmbeddedMessageDecodedByteBudget(t *testing.T) {
	for _, tt := range []struct {
		name, kind, want string
		data             []byte
		limit            int64
		wantErr          bool
	}{
		{name: "UTF16 ASCII exact fit", kind: "001F", data: utf16Bytes("1234567890"), limit: 10, want: "1234567890"},
		{name: "UTF16 terminated", kind: "001F", data: utf16Bytes("1234567890\x00"), limit: 10, want: "1234567890"},
		{name: "UTF16 surrogate pair", kind: "001F", data: utf16Bytes("Hi🙂"), limit: 6, want: "Hi🙂"},
		{name: "UTF8 expansion exact fit", kind: "001F", data: utf16Bytes("界界"), limit: 6, want: "界界"},
		{name: "UTF8 expansion over limit", kind: "001F", data: utf16Bytes("界界"), limit: 5, wantErr: true},
		{name: "ANSI terminated", kind: "001E", data: []byte("1234567890\x00"), limit: 10, want: "1234567890"},
		{name: "body over limit", kind: "001F", data: utf16Bytes("12345678901"), limit: 10, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msg := buildCFB([]cfbTestEntry{embeddedMSGAttachment(0, "inner.msg",
				cfbTestEntry{Name: "__substg1.0_1000" + tt.kind, Data: tt.data})})
			children, err := (msgContainer{}).Children(context.Background(), msg,
				ChildRequest{MaxFiles: 1, MaxBytes: tt.limit})
			if err != nil {
				t.Fatal(err)
			}
			if len(children) != 1 {
				t.Fatalf("children = %+v, want one attachment", children)
			}
			child := children[0]
			if (child.Err != nil) != tt.wantErr || string(child.Data) != tt.want {
				t.Fatalf("child = %+v, want text %q, error %v", child, tt.want, tt.wantErr)
			}
		})
	}
}

func TestEmbeddedMessageQuotaKeepsTheSameAttachment(t *testing.T) {
	// The storage order and display-name order deliberately differ. Neither
	// map iteration nor sorting returned error markers may change the grant.
	var entries []cfbTestEntry
	for i, name := range []string{"z.msg", "a.msg", "m.msg"} {
		entries = append(entries, embeddedMSGAttachment(i, name,
			cfbTestEntry{Name: "__substg1.0_0037001F", Data: utf16Bytes("Hi")}))
	}
	msg := buildCFB(entries)
	p := &Parser{containers: map[Format]Container{FormatMSG: msgContainer{}}}
	for i := 0; i < 100; i++ {
		node, err := p.Extract(context.Background(), msg, ExtractOptions{
			ReadOptions: ReadOptions{Name: "root.msg"}, MaxFiles: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		var accepted, stopped int
		for _, child := range node.Children {
			switch {
			case errors.Is(child.Err, errLimitReached):
				stopped++
			case child.Err != nil:
				t.Fatalf("unattempted attachment reported as a failed attempt: %+v", child)
			default:
				accepted++
				if child.Name != "z.txt" || child.Text != "Subject: Hi" {
					t.Fatalf("unexpected accepted attachment: %+v", child)
				}
			}
		}
		if accepted != 1 || stopped != 1 {
			t.Fatalf("accepted %d, stopped %d: %s", accepted, stopped, treeSummary(node))
		}
	}
}

func TestMSGAttachmentsShareOneBudget(t *testing.T) {
	message := func(index int) cfbTestEntry {
		return embeddedMSGAttachment(index, fmt.Sprintf("%d.msg", index),
			cfbTestEntry{Name: "__substg1.0_0037001F", Data: utf16Bytes("Hi")})
	}
	binary := cfbTestEntry{
		Name: "__attach_version1.0_#00000001",
		Children: []cfbTestEntry{
			{Name: "__substg1.0_3707001F", Data: utf16Bytes("1.txt")},
			{Name: "__substg1.0_37010102", Data: []byte("data")},
		},
	}
	for _, tt := range []struct {
		name    string
		entries []cfbTestEntry
		req     ChildRequest
		want    map[string]string
	}{
		{"file slots", []cfbTestEntry{message(0), message(1), message(2)}, ChildRequest{MaxFiles: 1, MaxBytes: 1000}, map[string]string{"0.txt": "Subject: Hi"}},
		{"bytes", []cfbTestEntry{message(0), message(1), message(2)}, ChildRequest{MaxBytes: 22}, map[string]string{"0.txt": "Subject: Hi", "1.txt": "Subject: Hi"}},
		{"mixed slots", []cfbTestEntry{message(0), binary, message(2)}, ChildRequest{MaxFiles: 2}, map[string]string{"0.txt": "Subject: Hi", "1.txt": "data"}},
		{"mixed bytes", []cfbTestEntry{message(0), binary, message(2)}, ChildRequest{MaxBytes: 15}, map[string]string{"0.txt": "Subject: Hi", "1.txt": "data"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			children, err := (msgContainer{}).Children(context.Background(), buildCFB(tt.entries), tt.req)
			if err != nil {
				t.Fatal(err)
			}
			accepted, stopped := 0, 0
			for _, child := range children {
				if errors.Is(child.Err, errLimitReached) {
					stopped++
					continue
				}
				want, ok := tt.want[child.Name]
				if child.Err != nil || !ok || string(child.Data) != want {
					t.Fatalf("unexpected child: %+v", child)
				}
				accepted++
			}
			if accepted != len(tt.want) || stopped != 1 {
				t.Fatalf("children = %+v, want %d accepted and one stop notice", children, len(tt.want))
			}
		})
	}
}

func TestRefusedEmbeddedMessageSpendsOneSlot(t *testing.T) {
	msg := buildCFB([]cfbTestEntry{
		embeddedMSGAttachment(0, "large.msg", cfbTestEntry{Name: "__substg1.0_0037001F", Data: utf16Bytes("Too long")}),
		embeddedMSGAttachment(1, "small.msg", cfbTestEntry{Name: "__substg1.0_0037001F", Data: utf16Bytes("Hi")}),
	})
	children, err := (msgContainer{}).Children(context.Background(), msg, ChildRequest{MaxFiles: 1, MaxBytes: 11})
	if err != nil {
		t.Fatal(err)
	}
	refused, stopped := 0, 0
	for _, child := range children {
		switch {
		case errors.Is(child.Err, errLimitReached):
			stopped++
		case child.Err != nil && child.Name == "large.txt":
			refused++
		default:
			t.Fatalf("unexpected child: %+v", child)
		}
	}
	if refused != 1 || stopped != 1 {
		t.Fatalf("children = %+v, want one refusal and one stop notice", children)
	}
}
