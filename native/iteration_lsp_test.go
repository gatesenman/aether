package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestIterationLSPFraming(t *testing.T) {
	for _, header := range []string{"Content-Length: nope\r\n\r\n", "Content-Length: 0\r\nContent-Length: 2\r\n\r\n{}", "Content-Length: 33554432\r\n\r\n"} {
		_, err := ReadLSPMessage(bufio.NewReader(strings.NewReader(header)))
		if err == nil || !strings.Contains(err.Error(), "Content-Length") {
			t.Errorf("invalid length did not fail at framing: %q: %v", header, err)
		}
	}
	var b bytes.Buffer
	if err := WriteLSPMessage(&b, map[string]any{"text": "中文𠀀"}); err != nil {
		t.Fatal(err)
	}
	msg, err := ReadLSPMessage(bufio.NewReader(&b))
	if err != nil || !json.Valid(msg) {
		t.Fatalf("valid unicode frame: %s %v", msg, err)
	}
	if err := WriteLSPMessage(&b, make(chan int)); err == nil {
		t.Error("marshal error ignored")
	}
}
func TestIterationLSPEditConsistency(t *testing.T) {
	const uri = "file:///iteration/main.ae"
	const src = `let 名 = "中𠀀文"`
	s := NewLSP(nil)
	s.docs[uri] = &lspDoc{URI: uri, Text: src, Version: 2}
	change := func(version int, changes []any) error {
		p, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": changes})
		_, err := s.Handle("textDocument/didChange", p)
		return err
	}
	if err := change(1, []any{map[string]any{"text": "stale"}}); err != nil {
		t.Fatal(err)
	}
	if s.docs[uri].Text != src {
		t.Error("stale edit overwrote current text")
	}
	s.docs[uri].Text = src
	s.docs[uri].Version = 2
	// valid replacement of a supplementary CJK character (two UTF-16 units).
	off := strings.Index(src, "𠀀")
	start := OffToPos(src, off)
	end := OffToPos(src, off+len("𠀀"))
	invalid := start
	invalid.Character++ // middle of UTF-16 surrogate pair
	err := change(3, []any{map[string]any{"text": src}, map[string]any{"range": LRange{invalid, end}, "text": "错"}})
	if err == nil || s.docs[uri].Text != src || s.docs[uri].Version != 2 {
		t.Error("invalid multi-edit was not rejected atomically")
	}
	if err := change(3, []any{map[string]any{"range": LRange{start, end}, "text": "好"}}); err != nil {
		t.Fatal(err)
	}
	if s.docs[uri].Text != strings.Replace(src, "𠀀", "好", 1) {
		t.Fatal("valid UTF-16 edit corrupted text")
	}
	current := s.docs[uri].Text
	if err := change(4, []any{map[string]any{"range": LRange{LPos{0, 5}, LPos{0, 1}}, "text": "bad"}}); err == nil {
		t.Error("reversed range accepted")
	}
	if s.docs[uri].Text != current {
		t.Error("invalid range changed text")
	}
}
