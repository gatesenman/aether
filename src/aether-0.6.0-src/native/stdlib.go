package main

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var EFFECTS = map[string]string{}

func parseNum(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil
}
func argErr(name string) { panic(aeErr("R031", 0, "bad arguments to %s", name)) }

func installStdlib(it *Interp) {
	g := it.Globals
	reg := func(name, eff string, f func(a []Value) Value) {
		g.Define(name, &Builtin{name, f, eff}, false)
		if eff != "" {
			EFFECTS[name] = eff
		}
	}
	ent := func(v Value) *EntityRef {
		e, ok := v.(*EntityRef)
		if !ok {
			panic(aeErr("R030", 0, "expected an entity, got %s", show(v)))
		}
		return e
	}
	list := func(name string, v Value) *List {
		l, ok := v.(*List)
		if !ok {
			argErr(name)
		}
		return l
	}
	recs := func(rs []*Record) *List {
		l := &List{Items: []Value{}}
		for _, r := range rs {
			l.Items = append(l.Items, r)
		}
		return l
	}
	call := func(f Value, a ...Value) Value { return it.Call(f, a, 0) }
	reg("print", "io", func(a []Value) Value {
		ps := make([]string, len(a))
		for i, x := range a {
			ps[i] = show(x)
		}
		it.Emit(strings.Join(ps, " "))
		return nil
	})
	reg("show", "", func(a []Value) Value { return show(a[0]) })
	reg("type_of", "", func(a []Value) Value { return typename(a[0]) })
	reg("len", "", func(a []Value) Value {
		switch x := a[0].(type) {
		case string:
			return int64(utf8.RuneCountInString(x))
		case *List:
			return int64(len(x.Items))
		case *Record:
			return int64(len(x.Keys))
		}
		panic(aeErr("R031", 0, "object of type %s has no len()", typename(a[0])))
	})
	reg("str", "", func(a []Value) Value { return show(a[0]) })
	reg("int", "", func(a []Value) Value {
		switch x := a[0].(type) {
		case int64:
			return x
		case float64:
			return int64(x)
		case bool:
			return b2i(x)
		case string:
			f, ok := parseNum(x)
			if !ok {
				panic(aeErr("R031", 0, "could not convert string to float: '%s'", x))
			}
			return int64(f)
		}
		argErr("int")
		return nil
	})
	reg("float", "", func(a []Value) Value {
		if s, ok := a[0].(string); ok {
			f, _ := parseNum(s)
			return f
		}
		f, _ := num(a[0])
		return f
	})
	reg("range", "", func(a []Value) Value {
		var lo, hi int64
		if len(a) == 1 {
			hi, _ = a[0].(int64)
		} else {
			lo, _ = a[0].(int64)
			hi, _ = a[1].(int64)
		}
		l := &List{Items: []Value{}}
		for i := lo; i < hi; i++ {
			l.Items = append(l.Items, i)
		}
		return l
	})
	reg("map", "", func(a []Value) Value {
		out := &List{Items: []Value{}}
		for _, x := range list("map", a[0]).Items {
			out.Items = append(out.Items, call(a[1], x))
		}
		return out
	})
	reg("filter", "", func(a []Value) Value {
		out := &List{Items: []Value{}}
		for _, x := range list("filter", a[0]).Items {
			if truthy(call(a[1], x)) {
				out.Items = append(out.Items, x)
			}
		}
		return out
	})
	reg("reduce", "", func(a []Value) Value {
		acc := a[2]
		for _, x := range list("reduce", a[0]).Items {
			acc = call(a[1], acc, x)
		}
		return acc
	})
	reg("sum", "", func(a []Value) Value {
		var si int64
		var sf float64
		isF := false
		for _, x := range list("sum", a[0]).Items {
			switch v := x.(type) {
			case int64:
				si += v
			case float64:
				sf += v
				isF = true
			case bool:
				si += b2i(v)
			default:
				panic(aeErr("R008", 0, "type error: cannot sum %s", typename(x)))
			}
		}
		if isF {
			return sf + float64(si)
		}
		return si
	})
	ext := func(name string, a []Value, wantMax bool) Value {
		items := a
		if len(a) == 1 {
			items = list(name, a[0]).Items
		}
		if len(items) == 0 {
			panic(aeErr("R031", 0, "%s() arg is an empty sequence", name))
		}
		best := items[0]
		for _, x := range items[1:] {
			lt, _ := valLess(best, x)
			gt, _ := valLess(x, best)
			if (wantMax && lt) || (!wantMax && gt) {
				best = x
			}
		}
		return best
	}
	reg("max", "", func(a []Value) Value { return ext("max", a, true) })
	reg("min", "", func(a []Value) Value { return ext("min", a, false) })
	reg("push", "", func(a []Value) Value { l := list("push", a[0]); l.Items = append(l.Items, a[1]); return l })
	reg("sort", "", func(a []Value) Value {
		src := list("sort", a[0]).Items
		items := append([]Value{}, src...)
		keys := items
		if len(a) > 1 && a[1] != nil {
			keys = make([]Value, len(items))
			for i, x := range items {
				keys[i] = call(a[1], x)
			}
		}
		idx := make([]int, len(items))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool { l, _ := valLess(keys[idx[i]], keys[idx[j]]); return l })
		out := &List{Items: []Value{}}
		for _, i := range idx {
			out.Items = append(out.Items, items[i])
		}
		return out
	})
	reg("reverse", "", func(a []Value) Value {
		if s, ok := a[0].(string); ok {
			r := []rune(s)
			for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
				r[i], r[j] = r[j], r[i]
			}
			return string(r)
		}
		src := list("reverse", a[0]).Items
		out := &List{Items: make([]Value, len(src))}
		for i, x := range src {
			out.Items[len(src)-1-i] = x
		}
		return out
	})
	reg("join", "", func(a []Value) Value {
		sep := ""
		if len(a) > 1 {
			sep, _ = a[1].(string)
		}
		ps := []string{}
		for _, x := range list("join", a[0]).Items {
			ps = append(ps, show(x))
		}
		return strings.Join(ps, sep)
	})
	reg("split", "", func(a []Value) Value {
		sep := " "
		if len(a) > 1 {
			sep, _ = a[1].(string)
		}
		s, _ := a[0].(string)
		out := &List{Items: []Value{}}
		for _, p := range strings.Split(s, sep) {
			out.Items = append(out.Items, p)
		}
		return out
	})
	reg("upper", "", func(a []Value) Value { s, _ := a[0].(string); return strings.ToUpper(s) })
	reg("lower", "", func(a []Value) Value { s, _ := a[0].(string); return strings.ToLower(s) })
	reg("contains", "", func(a []Value) Value {
		switch x := a[0].(type) {
		case string:
			s, _ := a[1].(string)
			return strings.Contains(x, s)
		case *List:
			for _, y := range x.Items {
				if valEq(y, a[1]) {
					return true
				}
			}
			return false
		case *Record:
			k, _ := a[1].(string)
			_, ok := x.M[k]
			return ok
		}
		return false
	})
	reg("keys", "", func(a []Value) Value {
		r, _ := a[0].(*Record)
		out := &List{Items: []Value{}}
		for _, k := range r.Keys {
			out.Items = append(out.Items, k)
		}
		return out
	})
	reg("values", "", func(a []Value) Value {
		r, _ := a[0].(*Record)
		out := &List{Items: []Value{}}
		for _, k := range r.Keys {
			out.Items = append(out.Items, r.M[k])
		}
		return out
	})
	reg("abs", "", func(a []Value) Value {
		if i, ok := a[0].(int64); ok {
			if i < 0 {
				return -i
			}
			return i
		}
		f, _ := num(a[0])
		return math.Abs(f)
	})
	reg("sqrt", "", func(a []Value) Value { f, _ := num(a[0]); return math.Sqrt(f) })
	reg("floor", "", func(a []Value) Value { f, _ := num(a[0]); return int64(math.Floor(f)) })
	reg("round", "", func(a []Value) Value {
		n := int64(0)
		if len(a) > 1 {
			n, _ = a[1].(int64)
		}
		if i, ok := a[0].(int64); ok {
			return i
		}
		f, _ := num(a[0])
		p := math.Pow(10, float64(n))
		return math.RoundToEven(f*p) / p
	})
	reg("random", "random", func(a []Value) Value { return rand.Float64() })
	reg("json", "", func(a []Value) Value { return toJSON(a[0]) })
	reg("parse_json", "", func(a []Value) Value {
		s, _ := a[0].(string)
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			panic(&AeThrow{"invalid json: " + err.Error()})
		}
		return fromJSON(v)
	})
	reg("error", "", func(a []Value) Value { panic(&AeThrow{a[0]}) })
	reg("assert", "", func(a []Value) Value {
		if !truthy(a[0]) {
			var m Value = "assertion failed"
			if len(a) > 1 {
				m = a[1]
			}
			panic(&AeThrow{m})
		}
		return nil
	})
	reg("now", "time", func(a []Value) Value { return time.Now().Format("2006-01-02 15:04:05") })
	reg("sleep", "time", func(a []Value) Value { f, _ := num(a[0]); time.Sleep(time.Duration(f) * time.Millisecond); return nil })
	reg("all_of", "async", func(a []Value) Value { return await(a[0]) })
	reg("http_get", "net", func(a []Value) Value {
		u, _ := a[0].(string)
		cl := http.Client{Timeout: 10 * time.Second}
		r, err := cl.Get(u)
		if err != nil {
			panic(&AeThrow{err.Error()})
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return string(bytes.ToValidUTF8(b, []byte("\uFFFD")))
	})
	st := it.Store
	reg("all", "db", func(a []Value) Value { return recs(st.All(ent(a[0]).Name)) })
	reg("get", "db", func(a []Value) Value {
		f, _ := num(a[1])
		if s, ok := a[1].(string); ok {
			f, _ = parseNum(s)
		}
		r := st.Get(ent(a[0]).Name, int64(f))
		if r == nil {
			return nil
		}
		return r
	})
	reg("count", "db", func(a []Value) Value { return int64(len(st.All(ent(a[0]).Name))) })
	reg("insert", "db", func(a []Value) Value {
		e := ent(a[0])
		var data *Record
		if len(a) > 1 {
			data, _ = a[1].(*Record)
		}
		r := it.NewRecord(e.Decl, data)
		it.CheckInv(e.Name, r)
		return st.Insert(e.Name, r)
	})
	reg("save", "db", func(a []Value) Value {
		e := ent(a[0])
		r, _ := a[1].(*Record)
		it.CheckInv(e.Name, r)
		if id, ok := r.M["id"].(int64); ok && id != 0 {
			return st.Update(e.Name, id, r)
		}
		return st.Insert(e.Name, r)
	})
	reg("remove", "db", func(a []Value) Value {
		e := ent(a[0])
		switch x := a[1].(type) {
		case *Record:
			id, _ := x.M["id"].(int64)
			st.Delete(e.Name, id)
		default:
			f, _ := num(x)
			st.Delete(e.Name, int64(f))
		}
		return nil
	})
	reg("where", "db", func(a []Value) Value {
		out := &List{Items: []Value{}}
		for _, r := range st.All(ent(a[0]).Name) {
			if truthy(call(a[1], r)) {
				out.Items = append(out.Items, r)
			}
		}
		return out
	})
	reg("run_flow", "flow", func(a []Value) Value {
		n, _ := a[0].(string)
		var ctx Value
		if len(a) > 1 {
			ctx = a[1]
		}
		return it.RunFlow(n, ctx)
	})
}
