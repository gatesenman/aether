package main

import (
	"fmt"
	"sort"
	"strings"
)

var builtinTypes = map[string]bool{}
var viewKinds = map[string]bool{}

func init() {
	for _, t := range strings.Fields("Int Float Text String Bool List Map Record Any Nil Fn Future Date Result Option") {
		builtinTypes[t] = true
	}
	for _, t := range strings.Fields("title text list table form button stat chart section row column card grid style image link") {
		viewKinds[t] = true
	}
}

type Diag = map[string]any
type Checker struct {
	diags    []Diag
	fns      map[string]Node
	types    map[string]bool
	ctors    map[string]int
	entities map[string]Node
	actions  map[string]Node
	globals  map[string]bool
	vty      map[string]string
	mods     map[string]map[string]bool
	BaseDir  string
	auth     Node
}
type ctxT struct {
	fn   Node
	used map[string]bool
}
type scopes []map[string]bool

func NewChecker() *Checker {
	c := &Checker{fns: map[string]Node{}, types: map[string]bool{}, ctors: map[string]int{}, entities: map[string]Node{}, actions: map[string]Node{}, globals: map[string]bool{}, vty: map[string]string{}, mods: map[string]map[string]bool{}, BaseDir: "."}
	for t := range builtinTypes {
		c.types[t] = true
	}
	it := NewInterp(":memory:", false)
	for n := range it.Globals.vars {
		c.globals[n] = true
	}
	return c
}
func (c *Checker) d(sev, code, msg string, line int, hint string) {
	for _, o := range c.diags {
		if o["code"] == code && o["line"] == line && o["message"] == msg {
			return
		}
	}
	x := Diag{"severity": sev, "code": code, "message": msg, "line": line, "col": 1}
	if hint != "" {
		x["hint"] = hint
	}
	c.diags = append(c.diags, x)
}
func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}
func paramNames(ps []any) map[string]bool {
	m := map[string]bool{}
	for _, p := range ps {
		m[p.(Node)["name"].(string)] = true
	}
	return m
}
func strs(xs []any) []string {
	o := []string{}
	for _, x := range xs {
		o = append(o, x.(string))
	}
	return o
}

func CheckSource(src string) (Node, []Diag) { return CheckSourceIn(src, ".") }
func CheckSourceIn(src, base string) (Node, []Diag) {
	prog, err := Parse(src)
	if err != nil {
		if e, ok := err.(*AeError); ok {
			return nil, []Diag{e.JSON()}
		}
		return nil, []Diag{{"severity": "error", "code": "E000", "message": err.Error(), "line": 0, "col": 1}}
	}
	c := NewChecker()
	c.BaseDir = base
	return prog, c.Check(prog)
}

func (c *Checker) Check(prog Node) []Diag {
	c.diags = []Diag{}
	decls := prog["decls"].([]any)
	for _, x := range decls {
		d := x.(Node)
		name, _ := d["name"].(string)
		switch d["k"] {
		case "Entity":
			c.entities[name] = d
			c.types[name] = true
			c.globals[name] = true
		case "TypeDecl":
			c.types[name] = true
			for _, v := range d["variants"].([]any) {
				vn := v.(Node)
				c.ctors[vn["name"].(string)] = len(vn["params"].([]any))
				c.globals[vn["name"].(string)] = true
			}
		case "Fn":
			c.fns[name] = d
			c.globals[name] = true
		case "Action":
			c.actions[name] = d
			c.globals[name] = true
		case "Let":
			c.globals[name] = true
		case "Import":
			names, mp, err := ModuleExports(c.BaseDir, d["path"].(string))
			if err != nil {
				c.d("error", "E260", err.Error(), ln(d), "check the path, or run `aether pkg install`")
				if al, _ := d["alias"].(string); al != "" {
					c.globals[al] = true
				}
				break
			}
			if al, _ := d["alias"].(string); al != "" {
				c.globals[al] = true
				c.mods[al] = names
			} else {
				for n := range names {
					c.globals[n] = true
				}
			}
			for _, y := range mp["decls"].([]any) {
				md := y.(Node)
				if md["k"] == "Entity" {
					c.entities[md["name"].(string)] = md
					c.types[md["name"].(string)] = true
					c.globals[md["name"].(string)] = true
				}
				if md["k"] == "TypeDecl" {
					c.types[md["name"].(string)] = true
				}
			}
		case "Auth":
			c.auth = d
		}
	}
	for _, x := range decls {
		d := x.(Node)
		c.vty = map[string]string{}
		switch d["k"] {
		case "Auth":
			c.checkAuth(d)
		case "Theme":
		case "Entity":
			seen := map[string]bool{}
			fset := map[string]bool{}
			for _, f := range d["fields"].([]any) {
				fset[f.(Node)["name"].(string)] = true
			}
			for _, iv := range d["invariants"].([]any) {
				c.infer(iv.(Node), scopes{fset}, nil)
			}
			for _, f := range d["fields"].([]any) {
				fn := f.(Node)
				nm := fn["name"].(string)
				if seen[nm] {
					c.d("error", "E210", fmt.Sprintf("duplicate field '%s' in entity %s", nm, d["name"]), ln(fn), "")
				}
				seen[nm] = true
				c.typeOk(fn["type"].(Node), ln(fn), nil)
				if fn["default"] != nil {
					t := c.infer(fn["default"].(Node), scopes{{}}, nil)
					if !c.compat(fn["type"].(Node), t) {
						c.d("error", "E220", fmt.Sprintf("default of '%s' is %s, expected %s", nm, t, fn["type"].(Node)["name"]), ln(fn), "")
					}
				}
			}
		case "Fn", "Action":
			tps := map[string]bool{}
			if tp, ok := d["tparams"].([]any); ok {
				for _, t := range tp {
					tps[t.(string)] = true
				}
			}
			for _, p := range d["params"].([]any) {
				if t, ok := p.(Node)["type"].(Node); ok {
					c.typeOk(t, ln(p.(Node)), tps)
					if c.entities[t["name"].(string)] != nil {
						c.vty[p.(Node)["name"].(string)] = t["name"].(string)
					}
				}
			}
			if d["k"] == "Action" {
				for _, r := range d["allow"].([]any) {
					c.checkRole(r.(string), ln(d))
				}
			}
			if r, ok := d["ret"].(Node); ok {
				c.typeOk(r, ln(d), tps)
			}
			var ctx *ctxT
			if d["k"] == "Fn" {
				ctx = &ctxT{d, map[string]bool{}}
			}
			pn := paramNames(d["params"].([]any))
			for _, r := range d["requires"].([]any) {
				c.infer(r.(Node), scopes{pn}, nil)
			}
			pr := map[string]bool{"result": true}
			for k := range pn {
				pr[k] = true
			}
			for _, r := range d["ensures"].([]any) {
				c.infer(r.(Node), scopes{pr}, nil)
			}
			t := c.block(d["body"].(Node), scopes{pn}, ctx)
			if r, ok := d["ret"].(Node); ok && !c.compat(r, t) {
				c.d("error", "E221", fmt.Sprintf("function '%s' returns %s but declares %s", d["name"], t, r["name"]), ln(d), "")
			}
			if ctx != nil {
				decl := set(strs(d["effects"].([]any))...)
				missing := []string{}
				for e := range ctx.used {
					if !decl[e] && e != "pure" {
						missing = append(missing, e)
					}
				}
				if len(missing) > 0 {
					sort.Strings(missing)
					all := map[string]bool{}
					for _, m := range missing {
						all[m] = true
					}
					for e := range decl {
						all[e] = true
					}
					al := []string{}
					for e := range all {
						al = append(al, e)
					}
					sort.Strings(al)
					declared := "none"
					if len(decl) > 0 {
						declared = pyList(strs(d["effects"].([]any)))
					}
					c.d("warning", "W300", fmt.Sprintf("function '%s' performs effects %s but declares %s", d["name"], pyList(missing), declared), ln(d),
						"add `! "+strings.Join(al, ", ")+"` after the signature")
				}
			}
		case "Test":
			c.block(d["body"].(Node), scopes{{}}, nil)
		case "Example":
			t := c.infer(d["expr"].(Node), scopes{{}}, nil)
			if t != "Bool" && t != "" {
				c.d("warning", "W320", "example should be a boolean expression, e.g. f(1) == 2", ln(d), "")
			}
		case "View":
			for _, x := range flattenItems(d["items"].([]any)) {
				it := x.(Node)
				kind := it["kind"].(string)
				args := it["args"].([]any)
				if !viewKinds[kind] {
					c.d("error", "E230", fmt.Sprintf("unknown view element '%s' (known: button, chart, form, list, section, stat, table, text, title)", kind), ln(it), "layout: row, column, grid, card { ... }; also image, link, style { ... }")
				}
				if (kind == "list" || kind == "table" || kind == "form" || kind == "chart") && len(args) > 0 {
					a0 := args[0].(Node)
					nm, _ := a0["name"].(string)
					if a0["k"] != "Name" || c.entities[nm] == nil {
						if nm == "" {
							nm = "?"
						}
						c.d("error", "E232", fmt.Sprintf("view element '%s' references unknown entity '%s'", kind, nm), ln(it), "")
					} else {
						fnames := []string{}
						fs := map[string]bool{}
						for _, f := range c.entities[nm]["fields"].([]any) {
							n := f.(Node)["name"].(string)
							if !fs[n] {
								fnames = append(fnames, n)
							}
							fs[n] = true
						}
						sort.Strings(fnames)
						for _, a := range args[1:] {
							an, _ := a.(Node)["name"].(string)
							if a.(Node)["k"] != "Name" || !fs[an] {
								if an == "" {
									an = "?"
								}
								c.d("error", "E233", fmt.Sprintf("entity %s has no field '%s'", nm, an), ln(it), "fields: "+strings.Join(fnames, ", "))
							}
						}
					}
					continue
				}
				for _, a := range args {
					an := a.(Node)
					if an["k"] == "Arrow" {
						if c.actions[an["target"].(string)] == nil {
							c.d("error", "E231", fmt.Sprintf("button targets unknown action '%s'", an["target"]), ln(it), "")
						}
					} else {
						c.infer(an, scopes{{}}, nil)
					}
				}
			}
		case "Flow":
			for _, s := range d["steps"].([]any) {
				c.block(s.(Node)["body"].(Node), scopes{{"ctx": true, "attempt": true}}, nil)
			}
		case "Module", "Import", "TypeDecl":
		default:
			c.stmt(d, scopes{{}}, nil)
		}
	}
	return c.diags
}
func pyList(xs []string) string {
	ps := []string{}
	for _, x := range xs {
		ps = append(ps, "'"+x+"'")
	}
	return "[" + strings.Join(ps, ", ") + "]"
}
func (c *Checker) typeOk(t Node, line int, tps map[string]bool) {
	n := t["name"].(string)
	if !c.types[n] && !tps[n] {
		c.d("error", "E201", fmt.Sprintf("unknown type '%s'", n), line, "")
	}
	for _, a := range t["args"].([]any) {
		c.typeOk(a.(Node), line, tps)
	}
}
func (c *Checker) compat(decl Node, t string) bool {
	if t == "" || t == "Any" || decl == nil {
		return true
	}
	n := decl["name"].(string)
	if t == "Nil" && decl["optional"].(bool) {
		return true
	}
	if n == "Any" || (len(n) == 1 && n[0] >= 'A' && n[0] <= 'Z') {
		return true
	}
	if n == "Float" && t == "Int" {
		return true
	}
	if n == "String" {
		n = "Text"
	}
	if c.entities[n] != nil && t == "Record" {
		return true
	}
	if c.entities[t] != nil && (n == "Record" || n == "Any" || n == t) {
		return true
	}
	if c.entities[t] != nil && builtinTypes[n] {
		return false
	}
	return n == t || !builtinTypes[n]
}
func (c *Checker) lookup(name string, sc scopes) bool {
	for _, s := range sc {
		if s[name] {
			return true
		}
	}
	return c.globals[name]
}
func (c *Checker) block(b Node, sc scopes, ctx *ctxT) string {
	sc = append(append(scopes{}, sc...), map[string]bool{})
	t := ""
	for _, st := range b["body"].([]any) {
		t = c.stmt(st.(Node), sc, ctx)
	}
	return t
}
func (c *Checker) stmt(st Node, sc scopes, ctx *ctxT) string {
	switch st["k"] {
	case "ExprStmt":
		return c.infer(st["expr"].(Node), sc, ctx)
	case "Let":
		t := c.infer(st["value"].(Node), sc, ctx)
		if ty, ok := st["type"].(Node); ok {
			c.typeOk(ty, ln(st), nil)
			if !c.compat(ty, t) {
				c.d("error", "E220", fmt.Sprintf("'%s' declared %s but value is %s", st["name"], ty["name"], t), ln(st), "")
			}
		}
		sc[len(sc)-1][st["name"].(string)] = true
		if ty, ok := st["type"].(Node); ok && c.entities[ty["name"].(string)] != nil {
			c.vty[st["name"].(string)] = ty["name"].(string)
		} else if c.entities[t] != nil {
			c.vty[st["name"].(string)] = t
		} else {
			delete(c.vty, st["name"].(string))
		}
	case "Assign":
		c.infer(st["target"].(Node), sc, ctx)
		c.infer(st["value"].(Node), sc, ctx)
	case "Return":
		if v, ok := st["value"].(Node); ok {
			return c.infer(v, sc, ctx)
		}
	case "While":
		c.infer(st["cond"].(Node), sc, ctx)
		c.block(st["body"].(Node), sc, ctx)
	case "For":
		c.infer(st["iter"].(Node), sc, ctx)
		if e := c.listOf(st["iter"].(Node)); e != "" {
			c.vty[st["var"].(string)] = e
		} else {
			delete(c.vty, st["var"].(string))
		}
		c.block(st["body"].(Node), append(append(scopes{}, sc...), set(st["var"].(string))), ctx)
	case "Try":
		c.block(st["body"].(Node), sc, ctx)
		c.block(st["handler"].(Node), append(append(scopes{}, sc...), set(st["var"].(string))), ctx)
	case "Throw":
		c.infer(st["value"].(Node), sc, ctx)
	}
	return ""
}
func (c *Checker) infer(e Node, sc scopes, ctx *ctxT) string {
	if e == nil {
		return ""
	}
	sub := func(k string) string { n, _ := e[k].(Node); return c.infer(n, sc, ctx) }
	switch e["k"] {
	case "Num":
		if _, ok := e["v"].(float64); ok {
			return "Float"
		}
		return "Int"
	case "Str":
		for _, p := range e["parts"].([]any) {
			if n, ok := p.(Node); ok {
				c.infer(n, sc, ctx)
			}
		}
		return "Text"
	case "Bool":
		return "Bool"
	case "Nil":
		return "Nil"
	case "Name":
		n := e["name"].(string)
		if !c.lookup(n, sc) {
			cands := []string{}
			for g := range c.globals {
				cands = append(cands, g)
			}
			for _, s := range sc {
				for x := range s {
					cands = append(cands, x)
				}
			}
			for e := range c.entities {
				cands = append(cands, e)
			}
			for a := range c.actions {
				cands = append(cands, a)
			}
			c.d("error", "E200", fmt.Sprintf("undefined name '%s'", n), ln(e), closest(n, cands))
		}
		if eff, ok := EFFECTS[n]; ok && ctx != nil {
			ctx.used[eff] = true
		}
		if t, ok := c.vty[n]; ok {
			return t
		}
		return ""
	case "List":
		for _, x := range e["items"].([]any) {
			c.infer(x.(Node), sc, ctx)
		}
		return "List"
	case "Record":
		for _, f := range e["fields"].([]any) {
			c.infer(f.([]any)[1].(Node), sc, ctx)
		}
		return "Record"
	case "Lambda":
		c.block(e["body"].(Node), append(append(scopes{}, sc...), paramNames(e["params"].([]any))), ctx)
		return "Fn"
	case "Unary":
		t := sub("e")
		if e["op"] == "not" {
			return "Bool"
		}
		return t
	case "Bin":
		l, r := sub("l"), sub("r")
		op := e["op"].(string)
		switch op {
		case "==", "!=", "<", ">", "<=", ">=", "and":
			return "Bool"
		case "or":
			return l
		}
		if op == "+" && (l == "Text" || r == "Text") {
			return "Text"
		}
		isN := func(t string) bool { return t == "Int" || t == "Float" }
		if isN(l) && isN(r) {
			if l == "Float" || r == "Float" || op == "/" {
				return "Float"
			}
			return "Int"
		}
		if l != "" && r != "" && !isN(l) && strings.Contains("-*/%", op) && l != "Any" {
			c.d("error", "E222", fmt.Sprintf("operator '%s' cannot apply to %s and %s", op, l, r), ln(e), "")
		}
		return ""
	case "Pipe":
		sub("l")
		r := e["r"].(Node)
		if r["k"] == "Call" {
			c.infer(r["fn"].(Node), sc, ctx)
			for _, a := range r["args"].([]any) {
				c.infer(a.(Node), sc, ctx)
			}
		} else {
			c.infer(r, sc, ctx)
		}
		return ""
	case "Call":
		args := e["args"].([]any)
		if f, ok := e["fn"].(Node); ok && f["k"] == "Name" && len(args) >= 2 {
			if a0, ok := args[0].(Node); ok && a0["k"] == "Name" && c.entities[a0["name"].(string)] != nil {
				en := a0["name"].(string)
				switch f["name"] {
				case "where", "filter", "sort", "map", "find", "any", "every", "sum_by":
					if lam, ok := args[1].(Node); ok && lam["k"] == "Lambda" {
						if ps := lam["params"].([]any); len(ps) >= 1 {
							c.vty[ps[0].(Node)["name"].(string)] = en
						}
					}
				case "insert", "update":
					if r, ok := args[len(args)-1].(Node); ok && r["k"] == "Record" {
						c.recordKeys(en, r)
					}
				}
			}
		}
		for _, a := range args {
			c.infer(a.(Node), sc, ctx)
		}
		f := e["fn"].(Node)
		if f["k"] == "Name" {
			n := f["name"].(string)
			local := false
			for _, s := range sc {
				if s[n] {
					local = true
				}
			}
			c.infer(f, sc, ctx)
			if fd, ok := c.fns[n]; ok && !local {
				if len(args) != len(fd["params"].([]any)) {
					c.d("error", "E240", fmt.Sprintf("'%s' expects %d arguments, got %d", n, len(fd["params"].([]any)), len(args)), ln(e), "")
				}
				if ctx != nil {
					for _, ef := range fd["effects"].([]any) {
						ctx.used[ef.(string)] = true
					}
				}
				if r, ok := fd["ret"].(Node); ok {
					return r["name"].(string)
				}
				return ""
			}
			if na, ok := c.ctors[n]; ok && !local && na != len(args) {
				c.d("error", "E240", fmt.Sprintf("constructor '%s' expects %d arguments, got %d", n, na, len(args)), ln(e), "")
			}
			if c.entities[n] != nil && !local {
				if len(args) == 1 {
					if r, ok := args[0].(Node); ok && r["k"] == "Record" {
						c.recordKeys(n, r)
					}
				}
				return n
			}
			if (n == "insert" || n == "get" || n == "save") && len(args) >= 1 && !local {
				if a0 := args[0].(Node); a0["k"] == "Name" && c.entities[a0["name"].(string)] != nil {
					return a0["name"].(string)
				}
			}
			switch n {
			case "len", "count", "int":
				return "Int"
			case "str", "show", "join", "upper", "lower", "now", "json":
				return "Text"
			case "all", "where", "map", "filter", "range", "sort", "split":
				return "List"
			}
			return ""
		}
		c.infer(f, sc, ctx)
		return ""
	case "Field":
		obj := e["obj"].(Node)
		fname := e["name"].(string)
		if obj["k"] == "Name" {
			if ex, ok := c.mods[obj["name"].(string)]; ok && !c.isLocal(obj["name"].(string), sc) {
				if !ex[fname] {
					keys := []string{}
					for k := range ex {
						keys = append(keys, k)
					}
					c.d("error", "E261", fmt.Sprintf("module '%s' has no exported name '%s'", obj["name"], fname), ln(e), closest(fname, keys))
				}
				return ""
			}
		}
		ot := sub("obj")
		if ent := c.entities[ot]; ent != nil {
			if fname == "id" {
				return "Int"
			}
			keys := []string{"id"}
			for _, f := range ent["fields"].([]any) {
				fn := f.(Node)
				if fn["name"] == fname {
					t := fn["type"].(Node)["name"].(string)
					if t == "String" {
						t = "Text"
					}
					if t == "Date" {
						return ""
					}
					return t
				}
				keys = append(keys, fn["name"].(string))
			}
			c.d("error", "E234", fmt.Sprintf("entity %s has no field '%s'", ot, fname), ln(e), closest(fname, keys))
		}
		return ""
	case "Index":
		sub("obj")
		sub("idx")
		return ""
	case "If":
		sub("cond")
		a := c.block(e["then"].(Node), sc, ctx)
		b := "Nil"
		if el, ok := e["els"].(Node); ok {
			b = c.block(el, sc, ctx)
		}
		if a == b {
			return a
		}
		return ""
	case "Match":
		sub("subj")
		ts := map[string]bool{}
		arms := e["arms"].([]any)
		hasCatch, hasCtor := false, false
		for _, a := range arms {
			arm := a.(Node)
			bs := map[string]bool{}
			pat := arm["pat"].(Node)
			c.pat(pat, bs, ln(e))
			if pat["k"] == "PWild" || pat["k"] == "PBind" {
				hasCatch = true
			}
			if pat["k"] == "PCtor" {
				hasCtor = true
			}
			s2 := append(append(scopes{}, sc...), bs)
			if g, ok := arm["guard"].(Node); ok {
				c.infer(g, s2, ctx)
			}
			b := arm["body"].(Node)
			if b["k"] == "Block" {
				ts[c.block(b, s2, ctx)] = true
			} else {
				ts[c.infer(b, s2, ctx)] = true
			}
		}
		if !hasCatch && !hasCtor {
			c.d("warning", "W310", "match may be non-exhaustive (add a `_ =>` arm)", ln(e), "")
		}
		if len(ts) == 1 {
			for t := range ts {
				return t
			}
		}
		return ""
	case "Spawn", "Await":
		if ctx != nil {
			ctx.used["async"] = true
		}
		sub("e")
		if e["k"] == "Spawn" {
			return "Future"
		}
		return ""
	}
	return ""
}
func (c *Checker) pat(p Node, bs map[string]bool, line int) {
	switch p["k"] {
	case "PBind":
		bs[p["name"].(string)] = true
	case "PCtor":
		if _, ok := c.ctors[p["name"].(string)]; !ok {
			c.d("error", "E250", fmt.Sprintf("unknown constructor '%s' in pattern", p["name"]), line, "")
		}
		for _, a := range p["args"].([]any) {
			c.pat(a.(Node), bs, line)
		}
	case "PList":
		for _, a := range p["items"].([]any) {
			c.pat(a.(Node), bs, line)
		}
	}
}

func (c *Checker) isLocal(n string, sc scopes) bool {
	for _, s := range sc {
		if s[n] {
			return true
		}
	}
	return false
}

// listOf returns the entity name if expression is a list of that entity (all/where/filter/sort).
func (c *Checker) listOf(e Node) string {
	if e["k"] != "Call" {
		return ""
	}
	f, _ := e["fn"].(Node)
	args := e["args"].([]any)
	if f == nil || f["k"] != "Name" || len(args) == 0 {
		return ""
	}
	switch f["name"] {
	case "all", "where", "filter", "sort", "take", "drop", "reverse":
		if a0 := args[0].(Node); a0["k"] == "Name" && c.entities[a0["name"].(string)] != nil {
			return a0["name"].(string)
		}
		if a0 := args[0].(Node); a0["k"] == "Call" {
			return c.listOf(a0)
		}
	}
	return ""
}

func (c *Checker) recordKeys(en string, r Node) {
	ent := c.entities[en]
	fs := map[string]Node{}
	keys := []string{"id"}
	for _, f := range ent["fields"].([]any) {
		fs[f.(Node)["name"].(string)] = f.(Node)
		keys = append(keys, f.(Node)["name"].(string))
	}
	for _, kv := range r["fields"].([]any) {
		k, _ := kv.([]any)[0].(string)
		if k == "" || k == "id" {
			continue
		}
		f, ok := fs[k]
		if !ok {
			c.d("error", "E234", fmt.Sprintf("entity %s has no field '%s'", en, k), ln(r), closest(k, keys))
			continue
		}
		_ = f
	}
}

func closest(bad string, cands []string) string {
	best, bd := "", 1<<30
	for _, cd := range cands {
		if d := editDist(strings.ToLower(bad), strings.ToLower(cd)); d < bd {
			best, bd = cd, d
		}
	}
	if best != "" && bd <= (len(bad)+2)/3+1 {
		return "did you mean '" + best + "'?"
	}
	if len(cands) > 0 {
		sort.Strings(cands)
		return "available: " + strings.Join(cands, ", ")
	}
	return ""
}

func (c *Checker) authRoles() map[string]bool {
	if c.auth == nil {
		return nil
	}
	m := map[string]bool{"guest": true}
	for _, r := range c.auth["roles"].([]any) {
		m[r.(string)] = true
	}
	return m
}
func (c *Checker) checkRole(r string, line int) {
	rs := c.authRoles()
	if rs != nil && !rs[r] {
		ks := []string{}
		for k := range rs {
			ks = append(ks, k)
		}
		c.d("error", "E271", fmt.Sprintf("unknown role '%s' (declared in auth: %s)", r, strings.Join(sortedKeys(rs), ", ")), line, closest(r, ks))
	}
}
func sortedKeys(m map[string]bool) []string {
	o := []string{}
	for k := range m {
		o = append(o, k)
	}
	sort.Strings(o)
	return o
}
func (c *Checker) checkAuth(a Node) {
	if len(a["roles"].([]any)) == 0 {
		c.d("error", "E270", "auth needs `roles <admin>, <user>...` (the first role is the administrator)", ln(a), "")
		return
	}
	if d := a["default"].(string); d != "" {
		c.checkRole(d, ln(a))
	}
	if s := a["signup"].(string); s != "open" && s != "closed" {
		c.d("error", "E272", "signup must be open or closed", ln(a), "")
	}
	if s := a["login"].(string); s != "optional" && s != "required" {
		c.d("error", "E272", "login must be optional or required", ln(a), "")
	}
	for _, x := range a["rules"].([]any) {
		r := x.(Node)
		for _, ro := range r["roles"].([]any) {
			c.checkRole(ro.(string), r["line"].(int))
		}
		for _, e := range r["entities"].([]any) {
			if c.entities[e.(string)] == nil {
				c.d("error", "E273", fmt.Sprintf("auth rule references unknown entity '%s'", e), r["line"].(int), "")
			}
		}
	}
}

// flattenItems expands layout containers and strips a trailing style record from each item.
func flattenItems(xs []any) []any {
	o := []any{}
	for _, x := range xs {
		it := x.(Node)
		args := it["args"].([]any)
		cp := Node{}
		for k, v := range it {
			cp[k] = v
		}
		if n := len(args); n > 0 && it["kind"] != "style" {
			if r, ok := args[n-1].(Node); ok && r["k"] == "Record" {
				cp["args"] = args[:n-1]
			}
		}
		o = append(o, cp)
		if ch, ok := it["children"].([]any); ok {
			o = append(o, flattenItems(ch)...)
		}
	}
	return o
}
