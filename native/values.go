package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Value = any
type List struct{ Items []Value }
type Record struct {
	Keys []string
	M    map[string]Value
}

func NewRec() *Record { return &Record{M: map[string]Value{}} }
func (r *Record) Get(k string) (Value, bool) { v, ok := r.M[k]; return v, ok }
func (r *Record) Set(k string, v Value) {
	if _, ok := r.M[k]; !ok {
		r.Keys = append(r.Keys, k)
	}
	r.M[k] = v
}
func (r *Record) Copy() *Record {
	n := NewRec()
	for _, k := range r.Keys {
		n.Set(k, r.M[k])
	}
	return n
}

type Closure struct {
	Name   string
	Params []any
	Body   Node
	Env    *Env
	Decl   Node
}
type Builtin struct {
	Name   string
	F      func(a []Value) Value
	Effect string
}
type Ctor struct {
	TName, Name string
	Fields      []string
}
type Variant struct {
	C    *Ctor
	Vals []Value
}
type EntityRef struct {
	Decl Node
	Name string
}
type Future struct {
	done chan struct{}
	val  Value
	err  any
}
type AeThrow struct{ V Value }

func (t *AeThrow) Error() string { return show(t.V) }

type Env struct {
	mu     sync.RWMutex
	vars   map[string]Value
	mut    map[string]bool
	parent *Env
}

func NewEnv(p *Env) *Env { return &Env{vars: map[string]Value{}, mut: map[string]bool{}, parent: p} }
func (e *Env) Get(n string, line int) Value {
	for x := e; x != nil; x = x.parent {
		x.mu.RLock()
		v, ok := x.vars[n]
		x.mu.RUnlock()
		if ok {
			return v
		}
	}
	panic(aeErr("R001", line, "undefined name '%s'", n))
}
func (e *Env) Has(n string) bool {
	for x := e; x != nil; x = x.parent {
		x.mu.RLock()
		_, ok := x.vars[n]
		x.mu.RUnlock()
		if ok {
			return true
		}
	}
	return false
}
func (e *Env) Define(n string, v Value, mutable bool) {
	e.mu.Lock()
	e.vars[n] = v
	if mutable {
		e.mut[n] = true
	} else {
		delete(e.mut, n)
	}
	e.mu.Unlock()
}
func (e *Env) Set(n string, v Value, line int) {
	for x := e; x != nil; x = x.parent {
		x.mu.Lock()
		if _, ok := x.vars[n]; ok {
			if !x.mut[n] {
				x.mu.Unlock()
				panic(aeErr("R002", line, "cannot reassign immutable '%s' (use var)", n))
			}
			x.vars[n] = v
			x.mu.Unlock()
			return
		}
		x.mu.Unlock()
	}
	panic(aeErr("R001", line, "undefined name '%s'", n))
}

// ---------- formatting (mirrors the Python reference `show` and json.dumps)
func fmtFloat(f float64) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	if math.IsNaN(f) {
		return "nan"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e16 {
		return strconv.FormatFloat(f, 'f', -1, 64) + ".0"
	}
	a := math.Abs(f)
	if a >= 1e16 || a < 1e-4 {
		s := strconv.FormatFloat(f, 'e', -1, 64)
		// python style: 1e+16, 1.5e-05
		mant, exp, _ := strings.Cut(s, "e")
		sign := exp[0]
		exp = strings.TrimLeft(exp[1:], "0")
		if len(exp) < 2 {
			exp = strings.Repeat("0", 2-len(exp)) + exp
		}
		return mant + "e" + string(sign) + exp
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
func jsonStr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
func show(v Value) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return fmtFloat(x)
	case string:
		return x
	case *List:
		parts := make([]string, len(x.Items))
		for i, it := range x.Items {
			if s, ok := it.(string); ok {
				parts[i] = jsonStr(s)
			} else {
				parts[i] = show(it)
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Record:
		parts := []string{}
		for _, k := range x.Keys {
			it := x.M[k]
			if s, ok := it.(string); ok {
				parts = append(parts, k+": "+jsonStr(s))
			} else {
				parts = append(parts, k+": "+show(it))
			}
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *Variant:
		if len(x.Vals) == 0 {
			return x.C.Name
		}
		ps := []string{}
		for _, a := range x.Vals {
			ps = append(ps, show(a))
		}
		return x.C.Name + "(" + strings.Join(ps, ", ") + ")"
	case *Closure:
		return "<fn " + x.Name + ">"
	case *Builtin:
		return "<builtin " + x.Name + ">"
	case *EntityRef:
		return "<entity " + x.Name + ">"
	case *Future:
		return "<future>"
	case *Ctor:
		return "<ctor " + x.Name + ">"
	}
	return fmt.Sprint(v)
}

// JSON with Python json.dumps(ensure_ascii=False) default formatting
func toJSON(v Value) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return jsonStr(x)
	case int64, int, float64:
		if f, ok := x.(float64); ok {
			if math.IsInf(f, 0) || math.IsNaN(f) {
				return "null"
			}
		}
		return show(x)
	case *List:
		ps := make([]string, len(x.Items))
		for i, it := range x.Items {
			ps[i] = toJSON(it)
		}
		return "[" + strings.Join(ps, ", ") + "]"
	case *Record:
		ps := []string{}
		for _, k := range x.Keys {
			ps = append(ps, jsonStr(k)+": "+toJSON(x.M[k]))
		}
		return "{" + strings.Join(ps, ", ") + "}"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ps := []string{}
		for _, k := range keys {
			ps = append(ps, jsonStr(k)+": "+toJSON(x[k]))
		}
		return "{" + strings.Join(ps, ", ") + "}"
	case []any:
		return toJSON(&List{x})
	case []string:
		l := &List{}
		for _, s := range x {
			l.Items = append(l.Items, s)
		}
		return toJSON(l)
	}
	return jsonStr(show(v))
}

// convert decoded JSON (UseNumber) into Aether values
func fromJSON(v any) Value {
	switch x := v.(type) {
	case map[string]any:
		r := NewRec()
		keys := make([]string, 0)
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			r.Set(k, fromJSON(x[k]))
		}
		return r
	case []any:
		l := &List{}
		for _, it := range x {
			l.Items = append(l.Items, fromJSON(it))
		}
		return l
	case interface{ String() string }:
		s := x.String()
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	case float64:
		if x == math.Trunc(x) {
			return int64(x)
		}
		return x
	}
	return v
}

func typename(v Value) string {
	switch v.(type) {
	case bool:
		return "Bool"
	case int64, int:
		return "Int"
	case float64:
		return "Float"
	case string:
		return "Text"
	case *List:
		return "List"
	case *Record:
		return "Record"
	case nil:
		return "Nil"
	case *Closure:
		return "Closure"
	case *Builtin:
		return "Builtin"
	case *Variant:
		return "Variant"
	case *EntityRef:
		return "EntityRef"
	}
	return "Any"
}
func truthy(v Value) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case *List:
		return len(x.Items) != 0
	}
	return true
}
func num(v Value) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}
func valEq(a, b Value) bool {
	if fa, ok := num(a); ok {
		if fb, ok2 := num(b); ok2 {
			return fa == fb
		}
		return false
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case *List:
		y, ok := b.(*List)
		if !ok || len(x.Items) != len(y.Items) {
			return false
		}
		for i := range x.Items {
			if !valEq(x.Items[i], y.Items[i]) {
				return false
			}
		}
		return true
	case *Record:
		y, ok := b.(*Record)
		if !ok || len(x.Keys) != len(y.Keys) {
			return false
		}
		for k, v := range x.M {
			w, ok := y.M[k]
			if !ok || !valEq(v, w) {
				return false
			}
		}
		return true
	case *Variant:
		y, ok := b.(*Variant)
		if !ok || x.C.Name != y.C.Name || len(x.Vals) != len(y.Vals) {
			return false
		}
		for i := range x.Vals {
			if !valEq(x.Vals[i], y.Vals[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// ordering; ok=false when incomparable
func valLess(a, b Value) (bool, bool) {
	if fa, ok := num(a); ok {
		if fb, ok2 := num(b); ok2 {
			return fa < fb, true
		}
		return false, false
	}
	if sa, ok := a.(string); ok {
		if sb, ok2 := b.(string); ok2 {
			return sa < sb, true
		}
	}
	if la, ok := a.(*List); ok {
		if lb, ok2 := b.(*List); ok2 {
			for i := 0; i < len(la.Items) && i < len(lb.Items); i++ {
				if valEq(la.Items[i], lb.Items[i]) {
					continue
				}
				return valLess(la.Items[i], lb.Items[i])
			}
			return len(la.Items) < len(lb.Items), true
		}
	}
	return false, false
}
