package main

import (
	"fmt"
	"regexp"
	"strings"
)

type Node = map[string]any

var prec = [][]string{{"or"}, {"and"}, {"==", "!=", "<", ">", "<=", ">="}, {"|>"}, {"+", "-"}, {"*", "/", "%"}}

type Parser struct {
	src   string
	t     []Tok
	i     int
	depth int
}

// Bound recursive syntax before it can exhaust the process stack (IDE/MCP inputs).
const maxParseDepth = 512

func (p *Parser) enter() {
	p.depth++
	if p.depth > maxParseDepth {
		t := p.peek(0)
		throw(&AeError{"E100", "syntax nesting limit exceeded", t.Line, t.Col})
	}
}
func (p *Parser) leave() { p.depth-- }

func throw(e *AeError) { panic(e) }

func (p *Parser) peek(o int) Tok {
	k := p.i + o
	if k >= len(p.t) {
		k = len(p.t) - 1
	}
	return p.t[k]
}
func (p *Parser) at(vals ...string) bool {
	t := p.peek(0)
	if t.Kind != "op" && t.Kind != "kw" {
		return false
	}
	for _, v := range vals {
		if t.S == v {
			return true
		}
	}
	return false
}
func (p *Parser) next() Tok { t := p.t[p.i]; p.i++; return t }
func (p *Parser) accept(vals ...string) bool {
	if p.at(vals...) {
		p.i++
		return true
	}
	return false
}
func (p *Parser) expect(v string) Tok {
	if !p.at(v) {
		t := p.peek(0)
		throw(&AeError{"E100", "expected '" + v + "' but found " + tokRepr(t), t.Line, t.Col})
	}
	return p.next()
}
func tokRepr(t Tok) string {
	if t.Kind == "eof" {
		return "None"
	}
	return "'" + t.S + "'"
}
func (p *Parser) ident() string {
	t := p.peek(0)
	if t.Kind != "id" {
		throw(&AeError{"E101", "expected identifier but found " + tokRepr(t), t.Line, t.Col})
	}
	p.i++
	return t.S
}
func nd(k string, line int, kv ...any) Node {
	n := Node{"k": k, "line": line}
	for i := 0; i+1 < len(kv); i += 2 {
		n[kv[i].(string)] = kv[i+1]
	}
	return n
}

func Parse(src string) (prog Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*AeError); ok {
				err = e
				return
			}
			panic(r)
		}
	}()
	toks, e := Lex(src)
	if e != nil {
		return nil, e
	}
	p := &Parser{src: src, t: toks}
	var decls []any
	for p.peek(0).Kind != "eof" {
		start := p.peek(0).Pos
		d := p.decl()
		d["span"] = []any{start, p.t[p.i-1].End}
		decls = append(decls, d)
		for p.accept(";") {
		}
	}
	return Node{"k": "Program", "decls": decls}, nil
}

func (p *Parser) decl() Node {
	t := p.peek(0)
	L := t.Line
	if p.accept("module") {
		return nd("Module", L, "name", p.ident())
	}
	if p.accept("import") {
		x := p.next()
		alias := ""
		if p.peek(0).Kind == "id" && p.peek(0).Val == "as" && p.peek(0).Line == x.Line {
			p.next()
			alias = p.ident()
		}
		imp := nd("Import", L, "path", x.Val)
		if alias != "" {
			imp["alias"] = alias
		}
		return imp
	}
	if p.accept("entity") {
		name := p.ident()
		p.expect("{")
		fields, invs := []any{}, []any{}
		for !p.accept("}") {
			if p.at("invariant") {
				p.next()
				invs = append(invs, p.expr(0))
				p.accept(",")
				continue
			}
			ft := p.peek(0)
			fn := p.ident()
			var ty Node
			if p.accept(":") {
				ty = p.type_()
			}
			var def any
			if p.accept("=") {
				def = p.expr(0)
			}
			if ty == nil { // v0.5 inference: `done = false` -> Bool, `title` -> Text, `owner_id` -> Int
				ty = Node{"k": "Type", "name": inferFieldType(fn, def), "args": []any{}, "optional": false}
			}
			fields = append(fields, nd("Field", ft.Line, "name", fn, "type", ty, "default", def))
			p.accept(",")
		}
		return nd("Entity", L, "name", name, "fields", fields, "invariants", invs)
	}
	if p.accept("type") {
		name := p.ident()
		tp := p.tparams()
		p.expect("=")
		vs := []any{}
		for {
			vt := p.peek(0)
			vn := p.ident()
			params := []any{}
			if p.at("(") {
				params = p.params()
			}
			vs = append(vs, nd("Variant", vt.Line, "name", vn, "params", params))
			if !p.accept("|") {
				break
			}
		}
		return nd("TypeDecl", L, "name", name, "tparams", tp, "variants", vs)
	}
	if p.at("fn") && p.peek(1).Kind == "id" {
		p.next()
		name := p.ident()
		tp := p.tparams()
		params := p.params()
		var ret any
		if p.accept("->") {
			ret = p.type_()
		}
		effs := []any{}
		if p.accept("!") {
			effs = append(effs, p.ident())
			for p.accept(",") {
				effs = append(effs, p.ident())
			}
		}
		req, ens := p.contracts()
		return nd("Fn", L, "name", name, "tparams", tp, "params", params, "ret", ret, "effects", effs, "requires", req, "ensures", ens, "body", p.block())
	}
	if p.accept("action") {
		name := p.ident()
		params := []any{}
		if p.at("(") {
			params = p.params()
		}
		allow := []any{}
		if p.accept("allow") {
			allow = append(allow, p.ident())
			for p.accept(",") {
				allow = append(allow, p.ident())
			}
		}
		req, ens := p.contracts()
		return nd("Action", L, "name", name, "params", params, "allow", allow, "requires", req, "ensures", ens, "body", p.block())
	}
	if p.accept("test") {
		n := p.next()
		return nd("Test", L, "name", n.S2(), "body", p.block())
	}
	if p.accept("example") {
		return nd("Example", L, "expr", p.expr(0))
	}
	if p.accept("view") {
		name := p.ident()
		p.expect("{")
		return nd("View", L, "name", name, "items", p.viewItems())
	}
	if p.peek(0).Kind == "id" && p.peek(0).Val == "auth" && p.peek(1).Val == "{" {
		p.next()
		p.expect("{")
		a := Node{"k": "Auth", "line": L, "roles": []any{}, "default": "", "signup": "open", "login": "optional", "rules": []any{}}
		words := func(line int) []any {
			xs := []any{}
			for !p.at("}") && p.peek(0).Line == line {
				t := p.next()
				if t.Val == "," {
					continue
				}
				xs = append(xs, fmt.Sprint(t.Val))
			}
			return xs
		}
		for !p.accept("}") {
			t := p.next()
			kw := fmt.Sprint(t.Val)
			ws := words(t.Line)
			switch kw {
			case "roles":
				a["roles"] = ws
			case "default", "signup", "login":
				if len(ws) != 1 {
					panic(aeErr("E101", t.Line, "auth: '%s' expects one word", kw))
				}
				a[kw] = ws[0]
			case "can":
				// can r1, r2 read|write E1, E2
				rs, op, es := []any{}, "", []any{}
				for _, w := range ws {
					ww := w.(string)
					if op == "" && (ww == "read" || ww == "write" || ww == "manage") {
						op = ww
					} else if op == "" {
						rs = append(rs, ww)
					} else {
						es = append(es, ww)
					}
				}
				if op == "" || len(rs) == 0 || len(es) == 0 {
					panic(aeErr("E101", t.Line, "auth: expected `can <roles> read|write <Entities>`"))
				}
				a["rules"] = append(a["rules"].([]any), Node{"roles": rs, "op": op, "entities": es, "line": t.Line})
			default:
				panic(aeErr("E101", t.Line, "auth: unknown setting '%s' (roles, default, signup, login, can)", kw))
			}
		}
		return a
	}
	if p.peek(0).Kind == "id" && p.peek(0).Val == "theme" && p.peek(1).Val == "{" {
		p.next()
		return nd("Theme", L, "props", p.expr(0))
	}
	if p.accept("flow") {
		name := p.ident()
		p.expect("{")
		steps := []any{}
		for !p.accept("}") {
			st := p.expect("step")
			sn := p.next()
			retry, backoff := 0.0, 200.0
			for !p.at("{") {
				w := p.next()
				switch w.Val {
				case "retry":
					retry = numVal(p.next().Val)
				case "backoff":
					backoff = numVal(p.next().Val)
					if u := p.peek(0); u.Kind == "id" && (u.Val == "s" || u.Val == "ms") {
						p.next()
						if u.Val == "s" {
							backoff *= 1000
						}
					}
				default:
					panic(aeErr("E101", w.Line, "step options: retry <n> backoff <n>ms|s"))
				}
			}
			step := nd("Step", st.Line, "name", sn.S2(), "body", p.block())
			if retry > 0 {
				step["retry"], step["backoff"] = int(retry), int(backoff)
			}
			steps = append(steps, step)
		}
		return nd("Flow", L, "name", name, "steps", steps)
	}
	return p.stmt()
}

var containerKinds = map[string]bool{"row": true, "column": true, "card": true, "grid": true}

func (p *Parser) viewItems() []any {
	p.enter()
	defer p.leave()
	items := []any{}
	for !p.accept("}") {
		it := p.peek(0)
		kind := p.ident()
		args := []any{}
		if containerKinds[kind] {
			for !p.at("{") {
				args = append(args, p.expr(0))
			}
			p.expect("{")
			items = append(items, nd("ViewItem", it.Line, "kind", kind, "args", args, "children", p.viewItems()))
			continue
		}
		for !(p.peek(0).Kind == "id" && p.peek(0).Line != it.Line) && !p.at("}") {
			if p.accept("->") {
				args = append(args, Node{"k": "Arrow", "target": p.ident()})
				continue
			}
			args = append(args, p.expr(0))
			if p.peek(0).Line != it.Line {
				break
			}
		}
		items = append(items, nd("ViewItem", it.Line, "kind", kind, "args", args))
	}
	return items
}

// S2: display value of token (string literal content or raw)
func (t Tok) S2() string {
	if s, ok := t.Val.(string); ok {
		return s
	}
	return t.S
}

func (p *Parser) contracts() ([]any, []any) {
	req, ens := []any{}, []any{}
	for p.at("requires", "ensures") {
		isReq := p.next().S == "requires"
		var lst []any
		lst = append(lst, p.expr(0))
		for p.accept(",") {
			lst = append(lst, p.expr(0))
		}
		if isReq {
			req = append(req, lst...)
		} else {
			ens = append(ens, lst...)
		}
	}
	return req, ens
}
func (p *Parser) tparams() []any {
	tp := []any{}
	if p.accept("<") {
		tp = append(tp, p.ident())
		for p.accept(",") {
			tp = append(tp, p.ident())
		}
		p.expect(">")
	}
	return tp
}
func (p *Parser) type_() Node {
	p.enter()
	defer p.leave()
	if p.accept("fn") {
		args := []any{}
		if p.accept("(") {
			for !p.accept(")") {
				args = append(args, p.type_())
				p.accept(",")
			}
		}
		if p.accept("->") {
			args = append(args, p.type_())
		}
		return Node{"k": "Type", "name": "Fn", "args": args, "optional": false}
	}
	name := p.ident()
	args := []any{}
	if p.accept("<") {
		args = append(args, p.type_())
		for p.accept(",") {
			args = append(args, p.type_())
		}
		p.expect(">")
	}
	opt := p.accept("?")
	return Node{"k": "Type", "name": name, "args": args, "optional": opt}
}
func (p *Parser) params() []any {
	p.expect("(")
	ps := []any{}
	for !p.accept(")") {
		pt := p.peek(0)
		n := p.ident()
		var ty any
		if p.accept(":") {
			ty = p.type_()
		}
		ps = append(ps, Node{"k": "Param", "name": n, "type": ty, "line": pt.Line})
		p.accept(",")
	}
	return ps
}
func (p *Parser) block() Node {
	p.enter()
	defer p.leave()
	t := p.expect("{")
	body := []any{}
	for !p.accept("}") {
		body = append(body, p.stmt())
		p.accept(";")
	}
	return nd("Block", t.Line, "body", body)
}
func (p *Parser) stmt() Node {
	t := p.peek(0)
	L := t.Line
	if p.at("let", "var") {
		mut := p.next().S == "var"
		name := p.ident()
		var ty any
		if p.accept(":") {
			ty = p.type_()
		}
		p.expect("=")
		return nd("Let", L, "name", name, "mutable", mut, "type", ty, "value", p.expr(0))
	}
	if p.accept("return") {
		var v any
		if !(p.at("}") || p.peek(0).Line != L) {
			v = p.expr(0)
		}
		return nd("Return", L, "value", v)
	}
	if p.accept("while") {
		c := p.expr(0)
		return nd("While", L, "cond", c, "body", p.block())
	}
	if p.accept("for") {
		v := p.ident()
		p.expect("in")
		it := p.expr(0)
		return nd("For", L, "var", v, "iter", it, "body", p.block())
	}
	if p.accept("try") {
		body := p.block()
		p.expect("catch")
		v := p.ident()
		return nd("Try", L, "body", body, "var", v, "handler", p.block())
	}
	if p.accept("throw") {
		return nd("Throw", L, "value", p.expr(0))
	}
	e := p.expr(0)
	if p.at("=", "+=", "-=") {
		op := p.next().S
		v := p.expr(0)
		if op != "=" {
			v = nd("Bin", L, "op", op[:1], "l", e, "r", v)
		}
		k := e["k"].(string)
		if k != "Name" && k != "Field" && k != "Index" {
			throw(&AeError{"E102", "invalid assignment target", L, t.Col})
		}
		return nd("Assign", L, "target", e, "value", v)
	}
	return nd("ExprStmt", L, "expr", e)
}
func (p *Parser) expr(lvl int) Node {
	if lvl == 0 {
		p.enter()
		defer p.leave()
	}
	if lvl == len(prec) {
		return p.unary()
	}
	l := p.expr(lvl + 1)
	for p.at(prec[lvl]...) {
		t := p.next()
		r := p.expr(lvl + 1)
		if t.S == "|>" {
			l = nd("Pipe", t.Line, "l", l, "r", r)
		} else {
			l = nd("Bin", t.Line, "op", t.S, "l", l, "r", r)
		}
	}
	return l
}
func (p *Parser) unary() Node {
	p.enter()
	defer p.leave()
	t := p.peek(0)
	if p.accept("-") {
		return nd("Unary", t.Line, "op", "-", "e", p.unary())
	}
	if p.accept("not", "!") {
		return nd("Unary", t.Line, "op", "not", "e", p.unary())
	}
	if p.accept("await") {
		return nd("Await", t.Line, "e", p.unary())
	}
	if p.accept("spawn") {
		return nd("Spawn", t.Line, "e", p.unary())
	}
	return p.postfix(p.primary())
}
func (p *Parser) postfix(e Node) Node {
	for {
		t := p.peek(0)
		prevLine := p.t[p.i-1].Line
		if p.at("(") && t.Line == prevLine {
			p.next()
			args := []any{}
			for !p.accept(")") {
				args = append(args, p.expr(0))
				p.accept(",")
			}
			e = nd("Call", t.Line, "fn", e, "args", args)
		} else if p.accept(".") {
			e = nd("Field", t.Line, "obj", e, "name", p.ident())
		} else if p.at("[") && t.Line == prevLine {
			p.next()
			idx := p.expr(0)
			p.expect("]")
			e = nd("Index", t.Line, "obj", e, "idx", idx)
		} else {
			return e
		}
	}
}
func (p *Parser) primary() Node {
	p.enter()
	defer p.leave()
	t := p.next()
	L := t.Line
	switch t.Kind {
	case "num":
		return nd("Num", L, "v", t.Val)
	case "str":
		return p.strlit(t)
	case "id":
		return nd("Name", L, "name", t.S)
	case "eof":
		throw(&AeError{"E103", "unexpected token None", L, t.Col})
	}
	switch t.S {
	case "true", "false":
		return nd("Bool", L, "v", t.S == "true")
	case "nil":
		return nd("Nil", L)
	case "(":
		e := p.expr(0)
		p.expect(")")
		return e
	case "[":
		items := []any{}
		for !p.accept("]") {
			items = append(items, p.expr(0))
			p.accept(",")
		}
		return nd("List", L, "items", items)
	case "{":
		fields := []any{}
		for !p.accept("}") {
			kt := p.next()
			p.expect(":")
			fields = append(fields, []any{kt.S2(), p.expr(0)})
			p.accept(",")
		}
		return nd("Record", L, "fields", fields)
	case "fn":
		params := p.params()
		var body Node
		if p.accept("=>") {
			body = nd("Block", L, "body", []any{nd("ExprStmt", L, "expr", p.expr(0))})
		} else {
			body = p.block()
		}
		return nd("Lambda", L, "params", params, "body", body)
	case "if":
		c := p.expr(0)
		th := p.block()
		var el any
		if p.accept("else") {
			if p.at("{") {
				el = p.block()
			} else {
				el = nd("Block", L, "body", []any{nd("ExprStmt", L, "expr", p.primary())})
			}
		}
		return nd("If", L, "cond", c, "then", th, "els", el)
	case "match":
		subj := p.expr(0)
		p.expect("{")
		arms := []any{}
		for !p.accept("}") {
			pat := p.pattern()
			var guard any
			if p.accept("if") {
				guard = p.expr(0)
			}
			p.expect("=>")
			var body Node
			if p.at("{") {
				body = p.block()
			} else {
				body = p.expr(0)
			}
			arms = append(arms, Node{"pat": pat, "guard": guard, "body": body})
			p.accept(",")
		}
		return nd("Match", L, "subj", subj, "arms", arms)
	}
	throw(&AeError{"E103", "unexpected token '" + t.S + "'", L, t.Col})
	return nil
}
func (p *Parser) pattern() Node {
	p.enter()
	defer p.leave()
	t := p.peek(0)
	if t.Kind == "num" || t.Kind == "str" || p.at("true", "false", "nil") {
		e := p.primary()
		if e["k"] == "Str" {
			return Node{"k": "PLit", "v": e["parts"].([]any)[0]}
		}
		return Node{"k": "PLit", "v": e["v"]}
	}
	if p.accept("-") {
		n := p.next()
		switch v := n.Val.(type) {
		case int64:
			return Node{"k": "PLit", "v": -v}
		case float64:
			return Node{"k": "PLit", "v": -v}
		}
	}
	if p.accept("[") {
		items := []any{}
		for !p.accept("]") {
			items = append(items, p.pattern())
			p.accept(",")
		}
		return Node{"k": "PList", "items": items}
	}
	name := p.ident()
	if name == "_" {
		return Node{"k": "PWild"}
	}
	if p.accept("(") {
		args := []any{}
		for !p.accept(")") {
			args = append(args, p.pattern())
			p.accept(",")
		}
		return Node{"k": "PCtor", "name": name, "args": args}
	}
	if r := []rune(name)[0]; r >= 'A' && r <= 'Z' {
		return Node{"k": "PCtor", "name": name, "args": []any{}}
	}
	return Node{"k": "PBind", "name": name}
}

var interpRe = regexp.MustCompile(`\{([^{}]*)\}`)

func (p *Parser) strlit(t Tok) Node {
	raw := t.Val.(string)
	unesc := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\x00LB", "{"), "\x00RB", "}") }
	if !strings.Contains(raw, "{") {
		return nd("Str", t.Line, "parts", []any{unesc(raw)})
	}
	parts := []any{}
	last := 0
	for _, m := range interpRe.FindAllStringSubmatchIndex(raw, -1) {
		parts = append(parts, unesc(raw[last:m[0]]))
		toks, err := Lex(raw[m[2]:m[3]])
		if err != nil {
			panic(err)
		}
		sub := &Parser{t: toks}
		parts = append(parts, sub.expr(0))
		last = m[1]
	}
	parts = append(parts, unesc(raw[last:]))
	return nd("Str", t.Line, "parts", parts)
}

func numVal(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	case int:
		return float64(x)
	}
	return 0
}
