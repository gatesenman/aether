package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"time"
)

// Content-addressed, attributed, reversible history — file format shared with the Python reference.
func stripAST(n any) any {
	switch x := n.(type) {
	case Node:
		o := map[string]any{}
		for k, v := range x {
			if k != "line" && k != "span" {
				o[k] = stripAST(v)
			}
		}
		return o
	case []any:
		o := make([]any, len(x))
		for i, v := range x {
			o[i] = stripAST(v)
		}
		return o
	}
	return n
}
func ASTHash(src string) string {
	canon := ""
	if p, err := Parse(src); err == nil {
		canon = toJSON(stripAST(p))
	} else {
		canon = "text:" + src
	}
	h := sha256.Sum256([]byte(canon))
	return hex.EncodeToString(h[:])[:12]
}

type HEntry struct {
	ID       int              `json:"id"`
	Hash     string           `json:"hash"`
	Parent   *string          `json:"parent"`
	Author   string           `json:"author"`
	Kind     string           `json:"kind"`
	Reason   string           `json:"reason"`
	Ops      []map[string]any `json:"ops"`
	Time     string           `json:"time"`
	Semantic bool             `json:"semantic_change"`
}
type History struct {
	Path    string            `json:"-"`
	Entries []HEntry          `json:"entries"`
	Objects map[string]string `json:"objects"`
	Pos     int               `json:"pos"`
}

func OpenHistory(path string) *History {
	h := &History{Path: path, Objects: map[string]string{}, Pos: -1, Entries: []HEntry{}}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			json.Unmarshal(b, h)
		}
	}
	return h
}
func (h *History) save() {
	if h.Path != "" {
		b, _ := json.MarshalIndent(h, "", " ")
		os.WriteFile(h.Path, b, 0644)
	}
}
func (h *History) Head() *HEntry {
	if h.Pos >= 0 && h.Pos < len(h.Entries) {
		return &h.Entries[h.Pos]
	}
	return nil
}
func (h *History) Commit(src, author, reason string, ops []map[string]any, kind string) HEntry {
	hs := ASTHash(src)
	head := h.Head()
	if head != nil && head.Hash == hs && h.Objects[head.Hash] == src {
		return *head
	}
	var parent *string
	if head != nil {
		p := head.Hash
		parent = &p
	}
	if ops == nil {
		ops = []map[string]any{}
	}
	e := HEntry{len(h.Entries) + 1, hs, parent, author, kind, reason, ops, time.Now().Format("2006-01-02 15:04:05"), !(head != nil && head.Hash == hs)}
	h.Objects[hs] = src
	h.Entries = append(h.Entries[:h.Pos+1], e)
	h.Pos = len(h.Entries) - 1
	h.save()
	return e
}
func (h *History) Undo() (string, error) {
	if h.Pos <= 0 {
		return "", aeErr("H001", 0, "nothing to undo")
	}
	h.Pos--
	h.save()
	return h.Objects[h.Head().Hash], nil
}
func (h *History) Redo() (string, error) {
	if h.Pos >= len(h.Entries)-1 {
		return "", aeErr("H002", 0, "nothing to redo")
	}
	h.Pos++
	h.save()
	return h.Objects[h.Head().Hash], nil
}
