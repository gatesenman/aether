package main

import (
	"math"
	"os"

	"strings"
	"sync"
)

type retSig struct{ v Value }

type Interp struct {
	Globals  *Env
	Entities map[string]Node
	EntOrder []string
	Views    []Node
	Actions  map[string]Node
	ActOrder []string
	Flows    map[string]Node
	FlowOrd  []string
	Tests    []Node
	Module   string
	Store    *Store
	Out      []string
	Echo     bool
	outMu    sync.Mutex
	BaseDir  string
	Lock     sync.Mutex
	declEnv  *Env
	Auth     Node
	Theme    Node
	loaded   map[string]*Record
	Hook     func(st Node, env *Env)
	OnEmit   func(string)
	Frames   []*DbgFrame
}

func NewInterp(dbPath string, echo bool) *Interp {
	st, err := OpenStore(dbPath)
	if err != nil {
		panic(err)
	}
	return newInterpWithStore(st, echo)
}

func newInterpWithStore(st *Store, echo bool) *Interp {
	it := &Interp{Globals: NewEnv(nil), Entities: map[string]Node{}, Actions: map[string]Node{}, Flows: map[string]Node{},
		Module: "main", Store: st, Echo: echo, BaseDir: "."}
	installStdlib(it)
	return it
}
func (it *Interp) Emit(s string) {
	it.outMu.Lock()
	it.Out = append(it.Out, s)
	it.outMu.Unlock()
	if it.OnEmit != nil {
		it.OnEmit(s)
	}
	if it.Echo {
		os.Stdout.WriteString(s + "\n")
	}
}

// catch converts panics into errors
func catch(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *AeError:
				err = e
			case *AeThrow:
				err = e
			case *retSig:
				err = nil
			default:
				panic(r)
			}
		}
	}()
	f()
	return nil
}

func (it *Interp) Load(src string, baseDir string) error {
	prog, err := Parse(src)
	if err != nil {
		return err
	}
	if baseDir != "" {
		it.BaseDir = baseDir
	}
	return catch(func() {
		decls := prog["decls"].([]any)
		for _, d := range decls {
			it.declare(d.(Node))
		}
		for _, n := range it.EntOrder {
			it.Store.Ensure(it.Entities[n])
		}
		for _, d := range decls {
			dn := d.(Node)
			switch dn["k"] {
			case "Module", "Import", "Entity", "TypeDecl", "Fn", "Action", "View", "Flow", "Test", "Example", "Auth", "Theme":
			default:
				it.exec(dn, it.Globals)
			}
		}
	})
}
func (it *Interp) declare(d Node) {
	g := it.Globals
	if it.declEnv != nil {
		g = it.declEnv
	}
	name, _ := d["name"].(string)
	switch d["k"] {
	case "Module":
		it.Module = name
	case "Import":
		it.importModule(d)
	case "Auth":
		it.Auth = d
	case "Theme":
		it.Theme = d
	case "Entity":
		if _, ok := it.Entities[name]; !ok {
			it.EntOrder = append(it.EntOrder, name)
		}
		it.Entities[name] = d
		g.Define(name, &EntityRef{d, name}, false)
	case "TypeDecl":
		for _, v := range d["variants"].([]any) {
			vn := v.(Node)
			c := &Ctor{TName: name, Name: vn["name"].(string)}
			for _, p := range vn["params"].([]any) {
				c.Fields = append(c.Fields, p.(Node)["name"].(string))
			}
			if len(c.Fields) == 0 {
				g.Define(c.Name, &Variant{C: c}, false)
			} else {
				g.Define(c.Name, c, false)
			}
		}
	case "Fn":
		g.Define(name, &Closure{name, d["params"].([]any), d["body"].(Node), g, d}, false)
	case "Test", "Example":
		it.Tests = append(it.Tests, d)
	case "Action":
		if _, ok := it.Actions[name]; !ok {
			it.ActOrder = append(it.ActOrder, name)
		}
		it.Actions[name] = d
		g.Define(name, &Closure{name, d["params"].([]any), d["body"].(Node), g, d}, false)
	case "View":
		it.Views = append(it.Views, d)
	case "Flow":
		if _, ok := it.Flows[name]; !ok {
			it.FlowOrd = append(it.FlowOrd, name)
		}
		it.Flows[name] = d
	}
}

func (it *Interp) execBlock(b Node, env *Env) Value {
	var r Value
	for _, st := range b["body"].([]any) {
		r = it.exec(st.(Node), env)
	}
	return r
}
func ln(n Node) int { l, _ := n["line"].(int); return l }

func (it *Interp) exec(st Node, env *Env) Value {
	if it.Hook != nil {
		it.Hook(st, env)
	}
	switch st["k"] {
	case "ExprStmt":
		return it.eval(st["expr"].(Node), env)
	case "Let":
		env.Define(st["name"].(string), it.eval(st["value"].(Node), env), st["mutable"].(bool))
		return nil
	case "Assign":
		v := it.eval(st["value"].(Node), env)
		t := st["target"].(Node)
		switch t["k"] {
		case "Name":
			env.Set(t["name"].(string), v, ln(st))
		case "Field":
			o := it.eval(t["obj"].(Node), env)
			r, ok := o.(*Record)
			if !ok {
				panic(aeErr("R003", ln(st), "field assignment on non-record"))
			}
			r.Set(t["name"].(string), v)
		default:
			o := it.eval(t["obj"].(Node), env)
			i := it.eval(t["idx"].(Node), env)
			switch c := o.(type) {
			case *List:
				n, _ := i.(int64)
				if n < 0 {
					n += int64(len(c.Items))
				}
				if n < 0 || int(n) >= len(c.Items) {
					panic(aeErr("R005", ln(st), "index %s out of range", show(i)))
				}
				c.Items[n] = v
			case *Record:
				c.Set(show(i), v)
			default:
				panic(aeErr("R003", ln(st), "index assignment on %s", typename(o)))
			}
		}
		return nil
	case "Return":
		var v Value
		if st["value"] != nil {
			v = it.eval(st["value"].(Node), env)
		}
		panic(&retSig{v})
	case "While":
		for truthy(it.eval(st["cond"].(Node), env)) {
			it.execBlock(st["body"].(Node), NewEnv(env))
		}
		return nil
	case "For":
		for _, x := range iterate(it.eval(st["iter"].(Node), env), ln(st)) {
			e := NewEnv(env)
			e.Define(st["var"].(string), x, true)
			it.execBlock(st["body"].(Node), e)
		}
		return nil
	case "Try":
		var res Value
		var caught Value
		failed := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					switch e := r.(type) {
					case *AeThrow:
						caught, failed = e.V, true
					case *AeError:
						caught, failed = e.Msg, true
					default:
						panic(r)
					}
				}
			}()
			res = it.execBlock(st["body"].(Node), NewEnv(env))
		}()
		if failed {
			e := NewEnv(env)
			e.Define(st["var"].(string), caught, true)
			return it.execBlock(st["handler"].(Node), e)
		}
		return res
	case "Throw":
		panic(&AeThrow{it.eval(st["value"].(Node), env)})
	}
	panic(aeErr("R010", ln(st), "cannot execute %v", st["k"]))
}

func iterate(v Value, line int) []Value {
	switch x := v.(type) {
	case *List:
		return append([]Value{}, x.Items...)
	case string:
		out := []Value{}
		for _, r := range x {
			out = append(out, string(r))
		}
		return out
	case *Record:
		out := []Value{}
		for _, k := range x.Keys {
			r := NewRec()
			r.Set("key", k)
			r.Set("value", x.M[k])
			out = append(out, r)
		}
		return out
	}
	panic(aeErr("R013", line, "cannot iterate over %s", typename(v)))
}

func (it *Interp) eval(e Node, env *Env) Value {
	switch e["k"] {
	case "Num", "Bool":
		return e["v"]
	case "Nil":
		return nil
	case "Str":
		var b strings.Builder
		for _, p := range e["parts"].([]any) {
			if s, ok := p.(string); ok {
				b.WriteString(s)
			} else {
				b.WriteString(show(it.eval(p.(Node), env)))
			}
		}
		return b.String()
	case "Name":
		return env.Get(e["name"].(string), ln(e))
	case "List":
		l := &List{Items: []Value{}}
		for _, x := range e["items"].([]any) {
			l.Items = append(l.Items, it.eval(x.(Node), env))
		}
		return l
	case "Record":
		r := NewRec()
		for _, f := range e["fields"].([]any) {
			kv := f.([]any)
			r.Set(kv[0].(string), it.eval(kv[1].(Node), env))
		}
		return r
	case "Lambda":
		return &Closure{"lambda", e["params"].([]any), e["body"].(Node), env, nil}
	case "Unary":
		v := it.eval(e["e"].(Node), env)
		if e["op"] == "-" {
			switch x := v.(type) {
			case int64:
				return -x
			case float64:
				return -x
			}
			panic(aeErr("R008", ln(e), "type error: cannot negate %s", typename(v)))
		}
		return !truthy(v)
	case "Bin":
		return it.binop(e, env)
	case "Pipe":
		l := it.eval(e["l"].(Node), env)
		r := e["r"].(Node)
		if r["k"] == "Call" {
			args := []Value{l}
			for _, a := range r["args"].([]any) {
				args = append(args, it.eval(a.(Node), env))
			}
			return it.Call(it.eval(r["fn"].(Node), env), args, ln(r))
		}
		return it.Call(it.eval(r, env), []Value{l}, ln(e))
	case "Call":
		fn := it.eval(e["fn"].(Node), env)
		args := []Value{}
		for _, a := range e["args"].([]any) {
			args = append(args, it.eval(a.(Node), env))
		}
		return it.Call(fn, args, ln(e))
	case "Field":
		o := it.eval(e["obj"].(Node), env)
		n := e["name"].(string)
		if r, ok := o.(*Record); ok {
			if v, ok := r.M[n]; ok {
				return v
			}
			panic(aeErr("R004", ln(e), "record has no field '%s'", n))
		}
		if v, ok := o.(*Variant); ok {
			for i, f := range v.C.Fields {
				if f == n {
					return v.Vals[i]
				}
			}
		}
		fn := it.Globals.Get(n, ln(e))
		line := ln(e)
		return &Builtin{Name: n, F: func(a []Value) Value { return it.Call(fn, append([]Value{o}, a...), line) }}
	case "Index":
		o := it.eval(e["obj"].(Node), env)
		i := it.eval(e["idx"].(Node), env)
		oor := func() { panic(aeErr("R005", ln(e), "index %s out of range", show(i))) }
		switch c := o.(type) {
		case *List:
			n, ok := i.(int64)
			if !ok {
				oor()
			}
			if n < 0 {
				n += int64(len(c.Items))
			}
			if n < 0 || int(n) >= len(c.Items) {
				oor()
			}
			return c.Items[n]
		case string:
			rs := []rune(c)
			n, ok := i.(int64)
			if !ok {
				oor()
			}
			if n < 0 {
				n += int64(len(rs))
			}
			if n < 0 || int(n) >= len(rs) {
				oor()
			}
			return string(rs[n])
		case *Record:
			k, _ := i.(string)
			if v, ok := c.M[k]; ok {
				return v
			}
		}
		oor()
	case "If":
		if truthy(it.eval(e["cond"].(Node), env)) {
			return it.execBlock(e["then"].(Node), NewEnv(env))
		}
		if e["els"] != nil {
			return it.execBlock(e["els"].(Node), NewEnv(env))
		}
		return nil
	case "Match":
		v := it.eval(e["subj"].(Node), env)
		for _, a := range e["arms"].([]any) {
			arm := a.(Node)
			b := map[string]Value{}
			if match(arm["pat"].(Node), v, b) {
				ae := NewEnv(env)
				for n, x := range b {
					ae.Define(n, x, true)
				}
				if arm["guard"] != nil && !truthy(it.eval(arm["guard"].(Node), ae)) {
					continue
				}
				body := arm["body"].(Node)
				if body["k"] == "Block" {
					return it.execBlock(body, ae)
				}
				return it.eval(body, ae)
			}
		}
		panic(aeErr("R006", ln(e), "no match arm for %s", show(v)))
	case "Spawn":
		inner := e["e"].(Node)
		f := &Future{done: make(chan struct{})}
		var run func() Value
		if inner["k"] == "Call" {
			fn := it.eval(inner["fn"].(Node), env)
			args := []Value{}
			for _, a := range inner["args"].([]any) {
				args = append(args, it.eval(a.(Node), env))
			}
			run = func() Value { return it.Call(fn, args, ln(e)) }
		} else {
			run = func() Value { return it.eval(inner, env) }
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					f.err = r
				}
				close(f.done)
			}()
			f.val = run()
		}()
		return f
	case "Await":
		v := it.eval(e["e"].(Node), env)
		return await(v)
	}
	panic(aeErr("R011", ln(e), "cannot evaluate %v", e["k"]))
}
func await(v Value) Value {
	if f, ok := v.(*Future); ok {
		<-f.done
		if f.err != nil {
			panic(f.err)
		}
		return f.val
	}
	if l, ok := v.(*List); ok {
		all := len(l.Items) > 0
		for _, x := range l.Items {
			if _, ok := x.(*Future); !ok {
				all = false
			}
		}
		if all {
			out := &List{}
			for _, x := range l.Items {
				out.Items = append(out.Items, await(x))
			}
			return out
		}
	}
	return v
}

func match(p Node, v Value, b map[string]Value) bool {
	switch p["k"] {
	case "PWild":
		return true
	case "PBind":
		b[p["name"].(string)] = v
		return true
	case "PLit":
		pv := p["v"]
		if vb, ok := v.(bool); ok {
			pb, ok2 := pv.(bool)
			return ok2 && pb == vb
		}
		return valEq(v, pv)
	case "PList":
		l, ok := v.(*List)
		items := p["items"].([]any)
		if !ok || len(l.Items) != len(items) {
			return false
		}
		for i, x := range items {
			if !match(x.(Node), l.Items[i], b) {
				return false
			}
		}
		return true
	case "PCtor":
		vr, ok := v.(*Variant)
		args := p["args"].([]any)
		if !ok || vr.C.Name != p["name"] || (len(args) != 0 && len(args) != len(vr.Vals)) {
			return false
		}
		for i, x := range args {
			if !match(x.(Node), vr.Vals[i], b) {
				return false
			}
		}
		return true
	}
	return false
}

func floorMod(a, b int64) int64 {
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}
func (it *Interp) binop(e Node, env *Env) Value {
	op := e["op"].(string)
	if op == "and" {
		return truthy(it.eval(e["l"].(Node), env)) && truthy(it.eval(e["r"].(Node), env))
	}
	if op == "or" {
		l := it.eval(e["l"].(Node), env)
		if truthy(l) {
			return l
		}
		return it.eval(e["r"].(Node), env)
	}
	l := it.eval(e["l"].(Node), env)
	r := it.eval(e["r"].(Node), env)
	terr := func() Value {
		panic(aeErr("R008", ln(e), "type error: cannot apply '%s' to %s and %s", op, typename(l), typename(r)))
	}
	switch op {
	case "==":
		return valEq(l, r)
	case "!=":
		return !valEq(l, r)
	case "<", ">", "<=", ">=":
		var lt, gt bool
		var ok bool
		if lt, ok = valLess(l, r); !ok {
			return terr()
		}
		gt, _ = valLess(r, l)
		switch op {
		case "<":
			return lt
		case ">":
			return gt
		case "<=":
			return !gt
		}
		return !lt
	}
	if op == "+" {
		ls, lok := l.(string)
		rs, rok := r.(string)
		if lok || rok {
			if !lok {
				ls = show(l)
			}
			if !rok {
				rs = show(r)
			}
			return ls + rs
		}
		if ll, ok := l.(*List); ok {
			if rl, ok := r.(*List); ok {
				return &List{append(append([]Value{}, ll.Items...), rl.Items...)}
			}
		}
	}
	if op == "*" {
		if s, ok := l.(string); ok {
			if n, ok := r.(int64); ok {
				if n < 0 {
					n = 0
				}
				return strings.Repeat(s, int(n))
			}
		}
	}
	li, lInt := l.(int64)
	ri, rInt := r.(int64)
	if lb, ok := l.(bool); ok {
		li, lInt = b2i(lb), true
	}
	if rb, ok := r.(bool); ok {
		ri, rInt = b2i(rb), true
	}
	if lInt && rInt {
		switch op {
		case "+":
			return li + ri
		case "-":
			return li - ri
		case "*":
			return li * ri
		case "/":
			if ri == 0 {
				panic(aeErr("R007", ln(e), "division by zero"))
			}
			if li%ri == 0 {
				return li / ri
			}
			return float64(li) / float64(ri)
		case "%":
			if ri == 0 {
				panic(aeErr("R007", ln(e), "division by zero"))
			}
			return floorMod(li, ri)
		}
	}
	lf, ok1 := num(l)
	rf, ok2 := num(r)
	if !ok1 || !ok2 {
		return terr()
	}
	switch op {
	case "+":
		return lf + rf
	case "-":
		return lf - rf
	case "*":
		return lf * rf
	case "/":
		if rf == 0 {
			panic(aeErr("R007", ln(e), "division by zero"))
		}
		return lf / rf
	case "%":
		if rf == 0 {
			panic(aeErr("R007", ln(e), "division by zero"))
		}
		m := math.Mod(lf, rf)
		if m != 0 && (m < 0) != (rf < 0) {
			m += rf
		}
		return m
	}
	return terr()
}
func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (it *Interp) Call(fn Value, args []Value, line int) Value {
	switch f := fn.(type) {
	case *Builtin:
		return f.F(args)
	case *Ctor:
		if len(args) != len(f.Fields) {
			panic(aeErr("R009", line, "%s expects %d args", f.Name, len(f.Fields)))
		}
		return &Variant{f, args}
	case *EntityRef:
		var data *Record
		if len(args) > 0 {
			data, _ = args[0].(*Record)
		}
		return it.NewRecord(f.Decl, data)
	case *Closure:
		if len(args) != len(f.Params) {
			panic(aeErr("R009", line, "%s expects %d args, got %d", f.Name, len(f.Params), len(args)))
		}
		env := NewEnv(f.Env)
		if it.Hook != nil {
			it.Frames = append(it.Frames, &DbgFrame{Name: f.Name, Env: env, Line: line})
			defer func() { it.Frames = it.Frames[:len(it.Frames)-1] }()
		}
		for i, p := range f.Params {
			env.Define(p.(Node)["name"].(string), args[i], true)
		}
		var req, ens []any
		if f.Decl != nil {
			req, _ = f.Decl["requires"].([]any)
			ens, _ = f.Decl["ensures"].([]any)
		}
		for _, c := range req {
			if !truthy(it.eval(c.(Node), env)) {
				panic(aeErr("R041", ln(c.(Node)), "precondition failed in %s: requires %s", f.Name, Unparse(c.(Node))))
			}
		}
		res := it.runBody(f.Body, env)
		if len(ens) > 0 {
			env.Define("result", res, true)
			for _, c := range ens {
				if !truthy(it.eval(c.(Node), env)) {
					panic(aeErr("R042", ln(c.(Node)), "postcondition failed in %s: ensures %s (result = %s)", f.Name, Unparse(c.(Node)), show(res)))
				}
			}
		}
		return res
	}
	panic(aeErr("R012", line, "%s is not callable", show(fn)))
}
func (it *Interp) runBody(b Node, env *Env) (res Value) {
	defer func() {
		if r := recover(); r != nil {
			if rs, ok := r.(*retSig); ok {
				res = rs.v
				return
			}
			panic(r)
		}
	}()
	return it.execBlock(b, env)
}

func (it *Interp) NewRecord(decl Node, data *Record) *Record {
	rec := NewRec()
	for _, f := range decl["fields"].([]any) {
		fn := f.(Node)
		name := fn["name"].(string)
		if data != nil {
			if v, ok := data.M[name]; ok {
				rec.Set(name, v)
				continue
			}
		}
		if fn["default"] != nil {
			rec.Set(name, it.eval(fn["default"].(Node), it.Globals))
			continue
		}
		switch fn["type"].(Node)["name"] {
		case "Int":
			rec.Set(name, int64(0))
		case "Float":
			rec.Set(name, 0.0)
		case "Bool":
			rec.Set(name, false)
		case "Text", "String":
			rec.Set(name, "")
		default:
			rec.Set(name, nil)
		}
	}
	if data != nil {
		if v, ok := data.M["id"]; ok {
			rec.Set("id", v)
		}
	}
	return rec
}
func (it *Interp) CheckInv(ename string, rec *Record) {
	decl := it.Entities[ename]
	invs, _ := decl["invariants"].([]any)
	if len(invs) == 0 {
		return
	}
	env := NewEnv(it.Globals)
	for _, f := range decl["fields"].([]any) {
		n := f.(Node)["name"].(string)
		env.Define(n, rec.M[n], true)
	}
	for _, c := range invs {
		if !truthy(it.eval(c.(Node), env)) {
			panic(aeErr("R040", ln(c.(Node)), "invariant of %s violated: %s", ename, Unparse(c.(Node))))
		}
	}
}

type PermErr struct{ Msg string }

func (it *Interp) RunAction(name string, payload *Record, role string) Value {
	a := it.Actions[name]
	if allow, _ := a["allow"].([]any); len(allow) > 0 {
		ok := false
		names := []string{}
		for _, r := range allow {
			names = append(names, r.(string))
			if r == role {
				ok = true
			}
		}
		if !ok {
			panic(&PermErr{"action '" + name + "' requires role " + strings.Join(names, " / ") + ", you are '" + role + "'"})
		}
	}
	type sv struct {
		tn  string
		rec *Record
	}
	var args []Value
	var toSave []sv
	for _, p := range a["params"].([]any) {
		pn := p.(Node)
		v := payload.M[pn["name"].(string)]
		tn := ""
		if t, ok := pn["type"].(Node); ok {
			tn = t["name"].(string)
		}
		if _, isEnt := it.Entities[tn]; isEnt {
			var id int64
			switch x := v.(type) {
			case int64:
				id = x
			case float64:
				id = int64(x)
			case string:
				f, _ := parseNum(x)
				id = int64(f)
			case *Record:
				id, _ = x.M["id"].(int64)
			}
			rec := it.Store.Get(tn, id)
			if rec == nil {
				panic(aeErr("R020", 0, "%s #%s not found", tn, show(v)))
			}
			toSave = append(toSave, sv{tn, rec})
			v = rec
		}
		args = append(args, v)
	}
	it.Lock.Lock()
	defer it.Lock.Unlock()
	r := it.Call(it.Globals.Get(name, 0), args, 0)
	for _, s := range toSave {
		it.CheckInv(s.tn, s.rec)
	}
	for _, s := range toSave {
		it.Store.Update(s.tn, s.rec.M["id"].(int64), s.rec)
	}
	return r
}

type TestResult struct {
	Name, Message string
	OK            bool
	Line          int
}

func (it *Interp) RunTests() []TestResult {
	res := []TestResult{}
	for _, t := range it.Tests {
		var name string
		if t["k"] == "Test" {
			name = t["name"].(string)
		} else {
			name = "example: " + Unparse(t["expr"].(Node))
		}
		ok, msg := true, ""
		err := catch(func() {
			if t["k"] == "Test" {
				it.execBlock(t["body"].(Node), NewEnv(it.Globals))
				return
			}
			ex := t["expr"].(Node)
			v := it.eval(ex, it.Globals)
			if !truthy(v) {
				ok = false
				if ex["k"] == "Bin" {
					msg = "left = " + show(it.eval(ex["l"].(Node), it.Globals))
				} else {
					msg = "got " + show(v)
				}
			}
		})
		if err != nil {
			ok = false
			if e, isA := err.(*AeError); isA {
				msg = "[" + e.Code + "] " + e.Msg
			} else {
				msg = err.Error()
			}
		}
		res = append(res, TestResult{name, msg, ok, ln(t)})
	}
	return res
}
