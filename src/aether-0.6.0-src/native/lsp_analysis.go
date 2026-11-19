package main

// Language analysis used by the LSP server: symbols, scopes, resolution, semantic classes.

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type Sym struct {
	Name, Kind, Parent, Detail string
	Tok                        Tok
	Scope                      [2]int // byte range where the symbol is visible
	Range                      [2]int // full declaration range
	Children                   []*Sym
}

type Analysis struct {
	Text    string
	Toks    []Tok
	Globals []*Sym
	Locals  []*Sym
	Fields  []*Sym
	All     []*Sym
	ByTok   map[int]*Sym // token Pos -> declared symbol
}

var builtinTypeNames = map[string]bool{"Int": true, "Float": true, "Text": true, "Bool": true, "Date": true, "List": true, "Map": true, "Any": true, "Option": true, "Result": true, "Nil": true}
var builtinNames = func() map[string]bool {
	m := map[string]bool{}
	it := NewInterp(":memory:", false)
	for n := range it.Globals.vars {
		m[n] = true
	}
	return m
}()

func Analyze(text string) *Analysis {
	a := &Analysis{Text: text, ByTok: map[int]*Sym{}}
	toks, err := Lex(text)
	if err != nil {
		toks = lexLenient(text)
	}
	for _, t := range toks {
		if t.Kind != "eof" {
			a.Toks = append(a.Toks, t)
		}
	}
	T := a.Toks
	n := len(T)
	is := func(i int, kind, s string) bool { return i >= 0 && i < n && T[i].Kind == kind && (s == "" || T[i].S == s) }
	match := func(i int, open, close string) int { // index of matching close for T[i]==open
		d := 0
		for j := i; j < n; j++ {
			if T[j].Kind == "op" && T[j].S == open {
				d++
			} else if T[j].Kind == "op" && T[j].S == close {
				d--
				if d == 0 {
					return j
				}
			}
		}
		return n - 1
	}
	add := func(s *Sym) *Sym {
		a.All = append(a.All, s)
		a.ByTok[s.Tok.Pos] = s
		return s
	}
	depth := 0
	var cur *Sym
	for i := 0; i < n; i++ {
		t := T[i]
		if t.Kind == "op" && (t.S == "{" || t.S == "}") {
			if t.S == "{" {
				depth++
			} else {
				depth--
			}
			continue
		}
		if cur != nil && t.Pos >= cur.Range[1] {
			cur = nil
		}
		if t.Kind != "kw" {
			continue
		}
		top := depth == 0
		switch t.S {
		case "module", "entity", "action", "view", "flow", "type":
			if !top || !is(i+1, "id", "") {
				continue
			}
		case "fn":
			if !top || !is(i+1, "id", "") {
				// lambda params
				if is(i+1, "op", "(") {
					e := match(i+1, "(", ")")
					for j := i + 2; j < e; j++ {
						if T[j].Kind == "id" && (is(j-1, "op", "(") || is(j-1, "op", ",")) {
							sc := [2]int{T[j].Pos, len(text)}
							if cur != nil {
								sc[1] = cur.Range[1]
							}
							a.Locals = append(a.Locals, add(&Sym{Name: T[j].S, Kind: "param", Tok: T[j], Scope: sc, Parent: "fn"}))
						}
					}
				}
				continue
			}
		case "let", "var", "for":
			if is(i+1, "id", "") {
				sc := [2]int{T[i+1].Pos, len(text)}
				if cur != nil {
					sc[1] = cur.Range[1]
				}
				k := "local"
				if t.S == "for" {
					k = "loopvar"
				}
				s := add(&Sym{Name: T[i+1].S, Kind: k, Tok: T[i+1], Scope: sc, Detail: lineOf(text, t.Pos)})
				if top && cur == nil {
					s.Kind = "global"
					s.Range = [2]int{t.Pos, lineEnd(text, t.Pos)}
					a.Globals = append(a.Globals, s)
				} else {
					a.Locals = append(a.Locals, s)
				}
			}
			continue
		case "test":
			if top && is(i+1, "str", "") {
				end := n - 1
				for j := i + 2; j < n; j++ {
					if is(j, "op", "{") {
						end = match(j, "{", "}")
						break
					}
				}
				s := &Sym{Name: strings.Trim(T[i+1].S, "\""), Kind: "test", Tok: T[i+1], Range: [2]int{t.Pos, T[end].End}}
				if v, ok := T[i+1].Val.(string); ok {
					s.Name = v
				}
				a.Globals = append(a.Globals, s)
			}
			continue
		default:
			continue
		}
		// top-level named declaration
		nameT := T[i+1]
		end := n - 1
		if t.S == "type" || t.S == "module" {
			le := lineEnd(text, t.Pos)
			for end = i + 1; end+1 < n && T[end+1].Pos < le; end++ {
			}
		} else {
			for j := i + 2; j < n; j++ {
				if is(j, "op", "{") {
					end = match(j, "{", "}")
					break
				}
			}
		}
		s := add(&Sym{Name: nameT.S, Kind: t.S, Tok: nameT, Scope: [2]int{0, len(text)}, Range: [2]int{t.Pos, T[end].End}})
		s.Detail = signature(text, T, i, end)
		a.Globals = append(a.Globals, s)
		cur = s
		switch t.S {
		case "fn", "action":
			if is(i+2, "op", "(") {
				e := match(i+2, "(", ")")
				for j := i + 3; j < e; j++ {
					if T[j].Kind == "id" && is(j+1, "op", ":") && (is(j-1, "op", "(") || is(j-1, "op", ",")) {
						p := add(&Sym{Name: T[j].S, Kind: "param", Tok: T[j], Scope: s.Range, Parent: s.Name, Detail: lineOf(text, T[j].Pos)})
						a.Locals = append(a.Locals, p)
					}
				}
			}
		case "entity":
			d := 0
			for j := i + 2; j < end; j++ {
				if is(j, "op", "{") {
					d++
				} else if is(j, "op", "}") {
					d--
				} else if d == 1 && T[j].Kind == "id" && is(j+1, "op", ":") && (j == 0 || T[j-1].Line != T[j].Line || is(j-1, "op", "{")) {
					f := add(&Sym{Name: T[j].S, Kind: "field", Tok: T[j], Parent: s.Name, Scope: [2]int{0, len(text)}, Range: [2]int{T[j].Pos, lineEnd(text, T[j].Pos)}, Detail: strings.TrimSpace(lineOf(text, T[j].Pos))})
					s.Children = append(s.Children, f)
					a.Fields = append(a.Fields, f)
				}
			}
		case "type":
			for j := i + 2; j <= end; j++ {
				if T[j].Kind == "id" && (is(j-1, "op", "=") || is(j-1, "op", "|")) {
					v := add(&Sym{Name: T[j].S, Kind: "variant", Tok: T[j], Parent: s.Name, Scope: [2]int{0, len(text)}, Range: [2]int{T[j].Pos, T[j].End}, Detail: s.Detail})
					s.Children = append(s.Children, v)
					a.Globals = append(a.Globals, v)
				}
			}
		case "flow":
			for j := i + 2; j < end; j++ {
				if is(j, "id", "step") || is(j, "kw", "step") {
					if is(j+1, "str", "") {
						nm, _ := T[j+1].Val.(string)
						s.Children = append(s.Children, &Sym{Name: nm, Kind: "step", Tok: T[j+1], Range: [2]int{T[j].Pos, lineEnd(text, T[j].Pos)}})
					}
				}
			}
		}
	}
	return a
}

// lexLenient lexes line by line so a broken line does not kill highlighting of the rest.
func lexLenient(text string) []Tok {
	var out []Tok
	off := 0
	for li, line := range strings.Split(text, "\n") {
		if ts, err := Lex(line); err == nil {
			for _, t := range ts {
				if t.Kind == "eof" {
					continue
				}
				t.Pos += off
				t.End += off
				t.Line = li + 1
				out = append(out, t)
			}
		}
		off += len(line) + 1
	}
	return out
}

func signature(text string, T []Tok, i, end int) string {
	start := T[i].Pos
	stop := T[end].End
	for j := i; j <= end; j++ {
		if T[j].Kind == "op" && T[j].S == "{" {
			if T[i].S != "entity" && T[i].S != "view" {
				stop = T[j].Pos
			}
			break
		}
	}
	s := strings.TrimSpace(text[start:stop])
	if ls := strings.Split(s, "\n"); len(ls) > 14 {
		s = strings.Join(ls[:14], "\n") + "\n  ..."
	}
	return s
}
func lineOf(text string, pos int) string {
	s := strings.LastIndex(text[:pos], "\n") + 1
	return text[s:lineEnd(text, pos)]
}
func lineEnd(text string, pos int) int {
	if e := strings.Index(text[pos:], "\n"); e >= 0 {
		return pos + e
	}
	return len(text)
}

// TokAt returns the index of the token covering byte offset off (or ending at it).
func (a *Analysis) TokAt(off int) int {
	for i, t := range a.Toks {
		if off >= t.Pos && off <= t.End && (t.Kind == "id" || t.Kind == "kw") {
			return i
		}
	}
	return -1
}

// Resolve finds the declaration for a name used at byte offset pos (prevDot: preceded by '.').
func (a *Analysis) Resolve(name string, pos int, prevDot bool) *Sym {
	if prevDot {
		for _, f := range a.Fields {
			if f.Name == name {
				return f
			}
		}
		return nil
	}
	var best *Sym
	for _, l := range a.Locals {
		if l.Name == name && pos >= l.Scope[0] && pos <= l.Scope[1] && l.Tok.Pos <= pos {
			if best == nil || l.Tok.Pos > best.Tok.Pos {
				best = l
			}
		}
	}
	if best != nil {
		return best
	}
	for _, g := range a.Globals {
		if g.Name == name && g.Kind != "test" {
			return g
		}
	}
	return nil
}

type Occ struct {
	Pos, End int
	Prev     bool
}

var identRe = regexp.MustCompile(`[A-Za-z_\x{4e00}-\x{9fff}][A-Za-z0-9_\x{4e00}-\x{9fff}]*`)

// Occurrences of identifiers (code tokens + names inside "{...}" string interpolations).
func (a *Analysis) Occurrences() []Occ {
	var out []Occ
	for i, t := range a.Toks {
		if t.Kind == "id" {
			out = append(out, Occ{t.Pos, t.End, i > 0 && a.Toks[i-1].Kind == "op" && a.Toks[i-1].S == "."})
		} else if t.Kind == "str" {
			src := a.Text[t.Pos:t.End]
			d := 0
			for j := 0; j < len(src); j++ {
				switch src[j] {
				case '\\':
					j++
				case '{':
					d++
					k := j + 1
					for k < len(src) && src[k] != '}' {
						k++
					}
					seg := src[j+1 : k]
					inQ := false
					for _, m := range identRe.FindAllStringIndex(seg, -1) {
						if strings.Count(seg[:m[0]], "\"")%2 == 1 || inQ {
							continue
						}
						prev := m[0] > 0 && seg[m[0]-1] == '.'
						w := seg[m[0]:m[1]]
						if keywords[w] {
							continue
						}
						out = append(out, Occ{t.Pos + j + 1 + m[0], t.Pos + j + 1 + m[1], prev})
					}
					j = k
					d--
				}
			}
		}
	}
	return out
}

// References returns all occurrences resolving to the same declaration as sym.
func (a *Analysis) References(sym *Sym, includeDecl bool) [][2]int {
	var out [][2]int
	for _, o := range a.Occurrences() {
		name := a.Text[o.Pos:o.End]
		if name != sym.Name {
			continue
		}
		if o.Pos == sym.Tok.Pos {
			if includeDecl {
				out = append(out, [2]int{o.Pos, o.End})
			}
			continue
		}
		if a.Resolve(name, o.Pos, o.Prev) == sym {
			out = append(out, [2]int{o.Pos, o.End})
		}
	}
	if includeDecl {
		found := false
		for _, r := range out {
			if r[0] == sym.Tok.Pos {
				found = true
			}
		}
		if !found && sym.Tok.End > sym.Tok.Pos {
			out = append(out, [2]int{sym.Tok.Pos, sym.Tok.End})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// SymAt returns the symbol referenced or declared at byte offset off.
func (a *Analysis) SymAt(off int) (*Sym, Occ, bool) {
	for _, o := range a.Occurrences() {
		if off >= o.Pos && off <= o.End {
			if s, ok := a.ByTok[o.Pos]; ok {
				return s, o, true
			}
			return a.Resolve(a.Text[o.Pos:o.End], o.Pos, o.Prev), o, true
		}
	}
	return nil, Occ{}, false
}

// ---- position helpers (LSP uses UTF-16 code units) ----
type LPos struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type LRange struct {
	Start LPos `json:"start"`
	End   LPos `json:"end"`
}

func OffToPos(text string, off int) LPos {
	if off > len(text) {
		off = len(text)
	}
	line := strings.Count(text[:off], "\n")
	ls := strings.LastIndex(text[:off], "\n") + 1
	return LPos{line, utf16Len(text[ls:off])}
}
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}
func PosToOff(text string, p LPos) int {
	off := 0
	for i := 0; i < p.Line; i++ {
		e := strings.Index(text[off:], "\n")
		if e < 0 {
			return len(text)
		}
		off += e + 1
	}
	c := 0
	for off < len(text) && text[off] != '\n' && c < p.Character {
		r, sz := utf8.DecodeRuneInString(text[off:])
		c += len(utf16.Encode([]rune{r}))
		off += sz
	}
	return off
}
func Rng(text string, a, b int) LRange { return LRange{OffToPos(text, a), OffToPos(text, b)} }
