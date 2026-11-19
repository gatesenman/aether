package main

// Agent-oriented projections (v0.5):
//   compact  : minimal-token canonical text (no comments/indentation, inferable field types omitted).
//              Lossless w.r.t. the program: ASTHash(expand(compact(x))) == ASTHash(x). Comments are not kept.
//   expand   : compact -> canonical human text (types restored, indented)
//   skeleton : signature-level summary for context windows (no bodies)
//   context  : the slice an agent needs to work on one symbol (decl + deps + contracts + users + skeleton)

import (
	"fmt"
	"sort"
	"strings"
)

// inferFieldType: convention-over-configuration for entity fields without an explicit type.
func inferFieldType(name string, def any) string {
	if d, ok := def.(Node); ok {
		switch d["k"] {
		case "Num":
			if _, f := d["v"].(float64); f {
				return "Float"
			}
			return "Int"
		case "Str":
			return "Text"
		case "Bool":
			return "Bool"
		case "List":
			return "List"
		case "Record":
			return "Record"
		case "Unary":
			if e, ok := d["e"].(Node); ok && e["k"] == "Num" {
				return inferFieldType(name, e)
			}
		}
		return "Any"
	}
	low := strings.ToLower(name)
	switch {
	case strings.HasSuffix(low, "_id") || low == "count" || low == "qty" || low == "quantity" || low == "age":
		return "Int"
	case strings.HasPrefix(low, "is_") || strings.HasPrefix(low, "has_") || low == "done" || low == "active" || low == "enabled":
		return "Bool"
	case low == "price" || low == "amount" || low == "total":
		return "Int"
	}
	return "Text"
}

func wordy(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)
	return isIdPart(r[0]) || isIdPart(r[len(r)-1]) || r[0] == '"' || r[0] >= 0x80
}

// fieldTypeSpans finds `: Type` spans in entity fields that inference would reproduce.
func droppableTypes(src string, prog Node) map[int][2]int {
	toks, _ := Lex(src)
	out := map[int][2]int{} // token index of ':' -> [startByte, endByte)
	depth, inEnt := 0, false
	entDepth := 0
	fields := map[int]Node{} // by line
	for _, x := range prog["decls"].([]any) {
		d := x.(Node)
		if d["k"] == "Entity" {
			for _, f := range d["fields"].([]any) {
				fields[f.(Node)["line"].(int)] = f.(Node)
			}
		}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind == "id" || t.Kind == "kw" {
			if t.S == "entity" {
				inEnt, entDepth = true, depth
			}
		}
		if t.S == "{" && t.Kind == "op" {
			depth++
		}
		if t.S == "}" && t.Kind == "op" {
			depth--
			if inEnt && depth == entDepth {
				inEnt = false
			}
		}
		if !inEnt || depth != entDepth+1 || t.Kind != "id" || i+3 >= len(toks) || toks[i+1].S != ":" {
			continue
		}
		f, ok := fields[t.Line]
		if !ok || f["name"] != t.S {
			continue
		}
		ty := f["type"].(Node)
		if len(ty["args"].([]any)) > 0 || ty["optional"].(bool) || toks[i+2].S != ty["name"] {
			continue
		}
		if inferFieldType(t.S, f["default"]) == ty["name"] {
			out[i+1] = [2]int{toks[i+1].Pos, toks[i+2].End}
		}
	}
	return out
}

func Compact(src string) (string, error) {
	prog, err := Parse(src)
	if err != nil {
		return "", err
	}
	drop := droppableTypes(src, prog)
	toks, _ := Lex(src)
	var b strings.Builder
	prev, line := "", 0
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind == "eof" {
			break
		}
		if _, ok := drop[i]; ok {
			i++ // skip ':' and the type name
			continue
		}
		txt := src[t.Pos:t.End]
		if line != 0 && t.Line != line {
			b.WriteString("\n")
			prev = ""
		} else if wordy(prev) && wordy(txt) {
			b.WriteString(" ")
		} else if prev == "-" && strings.HasPrefix(txt, "-") || prev == "+" && strings.HasPrefix(txt, "+") {
			b.WriteString(" ")
		}
		b.WriteString(txt)
		prev, line = txt, t.Line
	}
	return b.String() + "\n", nil
}

func Expand(src string) (string, error) {
	prog, err := Parse(src)
	if err != nil {
		return "", err
	}
	toks, _ := Lex(src)
	fields := map[string]Node{}
	for _, x := range prog["decls"].([]any) {
		d := x.(Node)
		if d["k"] == "Entity" {
			for _, f := range d["fields"].([]any) {
				fields[fmt.Sprint(f.(Node)["line"], ":", f.(Node)["name"])] = f.(Node)
			}
		}
	}
	// insert ": Type" after field names that have none
	ins := map[int]string{}
	depth, inEnt, entDepth := 0, false, 0
	for i, t := range toks {
		if t.S == "entity" {
			inEnt, entDepth = true, depth
		}
		if t.Kind == "op" && t.S == "{" {
			depth++
		} else if t.Kind == "op" && t.S == "}" {
			depth--
			if inEnt && depth == entDepth {
				inEnt = false
			}
		}
		if inEnt && depth == entDepth+1 && t.Kind == "id" && i+1 < len(toks) && toks[i+1].S != ":" && (i == 0 || toks[i-1].S == "{" || toks[i-1].S == "," || toks[i-1].Line != t.Line) {
			if f, ok := fields[fmt.Sprint(t.Line, ":", t.S)]; ok {
				ins[t.End] = ": " + f["type"].(Node)["name"].(string)
			}
		}
	}
	var b strings.Builder
	last := 0
	keys := []int{}
	for k := range ins {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		b.WriteString(src[last:k])
		b.WriteString(ins[k])
		last = k
	}
	b.WriteString(src[last:])
	s := prettyTokens(b.String())
	if _, err := Parse(s); err != nil || ASTHash(s) != ASTHash(b.String()) { // never change meaning
		s = b.String()
	}
	return Fmt(s)
}

func sigOf(d Node) string {
	ps := []string{}
	if pl, ok := d["params"].([]any); ok {
		for _, p := range pl {
			pn := p.(Node)
			s := pn["name"].(string)
			if t, ok := pn["type"].(Node); ok {
				s += ": " + typeStr(t)
			}
			ps = append(ps, s)
		}
	}
	s := d["name"].(string) + "(" + strings.Join(ps, ", ") + ")"
	if r, ok := d["ret"].(Node); ok {
		s += " -> " + typeStr(r)
	}
	if ef, _ := d["effects"].([]any); len(ef) > 0 {
		s += " ! " + strings.Join(strs(ef), ", ")
	}
	if al, _ := d["allow"].([]any); len(al) > 0 {
		s += " allow " + strings.Join(strs(al), ", ")
	}
	for _, r := range listOrNil(d["requires"]) {
		s += " requires " + Unparse(r.(Node))
	}
	for _, r := range listOrNil(d["ensures"]) {
		s += " ensures " + Unparse(r.(Node))
	}
	return s
}
func listOrNil(x any) []any { l, _ := x.([]any); return l }

func declSkeleton(d Node) string {
	name, _ := d["name"].(string)
	switch d["k"] {
	case "Module":
		return "module " + name
	case "Import":
		s := "import " + jsonStr(d["path"].(string))
		if a, _ := d["alias"].(string); a != "" {
			s += " as " + a
		}
		return s
	case "Entity":
		fs := []string{}
		for _, f := range d["fields"].([]any) {
			fn := f.(Node)
			x := fn["name"].(string) + ": " + typeStr(fn["type"].(Node))
			if fn["default"] != nil {
				x += " = " + Unparse(fn["default"].(Node))
			}
			fs = append(fs, x)
		}
		for _, iv := range d["invariants"].([]any) {
			fs = append(fs, "invariant "+Unparse(iv.(Node)))
		}
		return "entity " + name + " { " + strings.Join(fs, "; ") + " }"
	case "TypeDecl":
		vs := []string{}
		for _, v := range d["variants"].([]any) {
			vs = append(vs, v.(Node)["name"].(string))
		}
		return "type " + name + " = " + strings.Join(vs, " | ")
	case "Fn":
		return "fn " + sigOf(d)
	case "Action":
		return "action " + sigOf(d)
	case "Flow":
		ss := []string{}
		for _, s := range d["steps"].([]any) {
			ss = append(ss, jsonStr(s.(Node)["name"].(string)))
		}
		return "flow " + name + " { " + strings.Join(ss, " -> ") + " }"
	case "View":
		ks := []string{}
		for _, it := range flattenItems(d["items"].([]any)) {
			in := it.(Node)
			k := in["kind"].(string)
			if as := in["args"].([]any); len(as) > 0 {
				if a0 := as[0].(Node); a0["k"] == "Name" {
					k += " " + a0["name"].(string)
				}
			}
			ks = append(ks, k)
		}
		return "view " + name + " { " + strings.Join(ks, "; ") + " }"
	case "Auth":
		return "auth { roles " + strings.Join(strs(d["roles"].([]any)), ", ") + "; login " + d["login"].(string) + "; " + fmt.Sprint(len(d["rules"].([]any))) + " rules }"
	case "Theme":
		return "theme " + Unparse(d["props"].(Node))
	case "Test":
		return "test " + jsonStr(d["name"].(string))
	case "Example":
		return "example " + Unparse(d["expr"].(Node))
	case "Let":
		return "let " + name
	}
	return ""
}

func Skeleton(src string) (string, error) {
	prog, err := Parse(src)
	if err != nil {
		return "", err
	}
	out := []string{}
	tests := 0
	for _, x := range prog["decls"].([]any) {
		d := x.(Node)
		if d["k"] == "Test" || d["k"] == "Example" {
			tests++
			continue
		}
		if s := declSkeleton(d); s != "" {
			out = append(out, s)
		}
	}
	if tests > 0 {
		out = append(out, fmt.Sprintf("// %d tests/examples", tests))
	}
	return strings.Join(out, "\n") + "\n", nil
}

func declSource(src string, d Node) string {
	if sp, ok := d["span"].([]any); ok && len(sp) == 2 {
		a, _ := sp[0].(int)
		b, _ := sp[1].(int)
		if a >= 0 && b <= len(src) && a < b {
			return src[a:b]
		}
	}
	return declSkeleton(d)
}

// AgentContext: everything an agent needs to edit `symbol`, nothing more.
func AgentContext(src, symbol string, base string) (map[string]any, error) {
	prog, err := Parse(src)
	if err != nil {
		return nil, err
	}
	decls := map[string]Node{}
	order := []string{}
	for _, x := range prog["decls"].([]any) {
		d := x.(Node)
		if n, ok := d["name"].(string); ok && n != "" {
			if _, seen := decls[n]; !seen {
				order = append(order, n)
			}
			decls[n] = d
		}
	}
	target, ok := decls[symbol]
	if !ok {
		return nil, fmt.Errorf("no declaration named %s", symbol)
	}
	refs := func(d Node) map[string]bool {
		m := map[string]bool{}
		walkNames(d, func(s string) {
			if _, ok := decls[s]; ok && s != d["name"] {
				m[s] = true
			}
		})
		if d["k"] == "View" {
			for _, it := range flattenItems(d["items"].([]any)) {
				for _, a := range it.(Node)["args"].([]any) {
					if a.(Node)["k"] == "Arrow" {
						m[a.(Node)["target"].(string)] = true
					}
				}
			}
		}
		return m
	}
	deps := refs(target)
	users := []string{}
	for _, n := range order {
		if n != symbol && refs(decls[n])[symbol] {
			users = append(users, n)
		}
	}
	depSrc := []string{}
	for _, n := range sortedKeys(deps) {
		depSrc = append(depSrc, declSkeleton(decls[n]))
	}
	contracts := []any{}
	for _, c := range contractsOf(src) {
		if cm, ok := c.(map[string]any); ok && (cm["owner"] == symbol || deps[fmt.Sprint(cm["owner"])]) {
			contracts = append(contracts, cm)
		}
	}
	_, diags := CheckSourceIn(src, base)
	rel := []any{}
	for _, d := range diags {
		rel = append(rel, map[string]any(d))
	}
	sk, _ := Skeleton(src)
	text := "// target\n" + declSource(src, target) + "\n// dependencies (signatures)\n" + strings.Join(depSrc, "\n") +
		"\n// used by: " + strings.Join(users, ", ") + "\n// rest of program (skeleton)\n" + sk
	return map[string]any{"symbol": symbol, "kind": target["k"], "source": declSource(src, target), "dependencies": depSrc,
		"usedBy": users, "contracts": contracts, "diagnostics": rel, "text": text, "ops": OpsSchema}, nil
}

var binOps = map[string]bool{"==": true, "!=": true, "<=": true, ">=": true, "+=": true, "-=": true, "=>": true, "->": true, "|>": true, "=": true, "<": true, ">": true, "+": true, "*": true, "/": true, "%": true, "-": true, "|": true}

// prettyTokens re-spaces a token stream (line structure is kept; Fmt does indentation).
func prettyTokens(src string) string {
	toks, err := Lex(src)
	if err != nil {
		return src
	}
	var b strings.Builder
	var prev *Tok
	for i := range toks {
		t := &toks[i]
		if t.Kind == "eof" {
			break
		}
		txt := src[t.Pos:t.End]
		if prev != nil && prev.Line != t.Line {
			b.WriteString("\n")
		} else if prev != nil {
			ps := src[prev.Pos:prev.End]
			sp := false
			valueEnd := wordy(ps) || ps == ")" || ps == "]" || ps == "}"
			switch {
			case txt == "," || txt == ";" || txt == ")" || txt == "]" || txt == ":" || txt == "." || ps == "." || ps == "(" || ps == "[":
				sp = false
			case txt == "(" || txt == "[":
				sp = !valueEnd || (prev.Kind == "kw")
			case t.Kind == "op" && binOps[txt]:
				sp = valueEnd || (prev.Kind == "op" && binOps[ps])
			case prev.Kind == "op" && binOps[ps]:
				// unary minus: no space after it if it wasn't binary
				sp = !(ps == "-" && i >= 2 && !(wordy(src[toks[i-2].Pos:toks[i-2].End]) || strings.Contains(")]}", src[toks[i-2].Pos:toks[i-2].End])))
			case ps == "," || ps == ";" || ps == ":" || ps == "{" || txt == "{" || txt == "}" || ps == "}":
				sp = true
			case wordy(ps) && wordy(txt), ps == ")" && wordy(txt), ps == "!" && valueEnd:
				sp = true
			case txt == "!":
				sp = ps == ")" || prev.Kind == "id" && i+1 < len(toks) && toks[i+1].Kind == "id" && src[toks[i+1].Pos:toks[i+1].End] != "" && prevIsSig(toks, i)
			}
			if sp {
				b.WriteString(" ")
			}
		}
		b.WriteString(txt)
		prev = t
	}
	return b.String() + "\n"
}

func prevIsSig(toks []Tok, i int) bool { return i > 0 && toks[i-1].S == "->" || i > 1 && toks[i-2].S == "->" }
