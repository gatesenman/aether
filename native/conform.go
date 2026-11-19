package main

// `aether conform golden.json`: runs the conformance suite generated from the Python reference spec.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func canon(v any) any {
	// round-trip through our Python-compatible JSON encoder, then decode with numbers kept as text
	var s string
	if b, ok := v.(json.RawMessage); ok {
		s = string(b)
	} else {
		s = toJSON(v)
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var out any
	dec.Decode(&out)
	return numText(out)
}
func numText(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, y := range x {
			x[k] = numText(y)
		}
	case []any:
		for i, y := range x {
			x[i] = numText(y)
		}
	case json.Number:
		return "#" + x.String()
	}
	return v
}
func stripMs(v Value) Value {
	switch x := v.(type) {
	case *Record:
		n := NewRec()
		for _, k := range x.Keys {
			if k != "ms" {
				n.Set(k, stripMs(x.M[k]))
			}
		}
		return n
	case *List:
		l := &List{}
		for _, y := range x.Items {
			l.Items = append(l.Items, stripMs(y))
		}
		return l
	}
	return v
}
func errNorm(err error) string {
	switch e := err.(type) {
	case *AeThrow:
		return "uncaught: " + show(e.V)
	case *AeError:
		return e.Code + ": " + e.Msg
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func runConform(golden string) int {
	raw, err := os.ReadFile(golden)
	if err != nil {
		return fatal(err)
	}
	root := filepath.Dir(filepath.Dir(golden))
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	var cases []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cases); err != nil {
		return fatal(err)
	}
	pass, fail := 0, 0
	for i, c := range cases {
		var kind, file string
		json.Unmarshal(c["kind"], &kind)
		json.Unmarshal(c["file"], &file)
		src := ""
		if file != "" {
			b, err := os.ReadFile(filepath.Join(root, file))
			if err != nil {
				fmt.Printf("FAIL [%d] %s %s: %v\n", i, kind, file, err)
				fail++
				continue
			}
			src = string(b)
		} else {
			json.Unmarshal(c["source"], &src)
		}
		dir := filepath.Dir(filepath.Join(root, file))
		var actual any
		func() {
			defer func() {
				if r := recover(); r != nil {
					actual = map[string]any{"panic": fmt.Sprint(r)}
				}
			}()
			switch kind {
			case "run":
				it := NewInterp(":memory:", false)
				err := it.Load(src, dir)
				out := ""
				for _, l := range it.Out {
					out += l + "\n"
				}
				code := int64(0)
				if err != nil {
					code = 1
				}
				r := NewRec()
				r.Set("stdout", out)
				r.Set("exit", code)
				r.Set("error", errNorm(err))
				actual = r
			case "check":
				_, d := CheckSource(src)
				actual = diagList(d)
			case "hash":
				actual = ASTHash(src)
			case "fmt":
				actual, _ = Fmt(src)
			case "explain":
				actual, _ = Explain(src)
			case "test":
				it := NewInterp(":memory:", false)
				it.Load(src, dir)
				l := []any{}
				for _, t := range it.RunTests() {
					l = append(l, []any{t.Name, t.OK, t.Message})
				}
				actual = l
			case "api":
				app, err := NewApp(src, ":memory:", dir)
				if err != nil {
					actual = err.Error()
					return
				}
				var steps [][]json.RawMessage
				json.Unmarshal(c["steps"], &steps)
				l := []any{}
				for _, st := range steps {
					var m, p, role string
					json.Unmarshal(st[0], &m)
					json.Unmarshal(st[1], &p)
					json.Unmarshal(st[3], &role)
					var body []byte
					if string(st[2]) != "null" {
						body = st[2]
					}
					code, ct, out := app.Handle(m, p, body, role)
					var v any
					if strings.Contains(ct, "json") {
						v = stripMs(decodeAny(out))
					}
					l = append(l, []any{int64(code), v})
				}
				actual = l
			case "ops":
				var ops []map[string]any
				json.Unmarshal(c["ops"], &ops)
				r, err := ApplyOps(src, ops)
				if err != nil {
					actual = err.Error()
					return
				}
				actual = map[string]any{"source": r.Source, "ok": r.OK}
			case "propose":
				var prompt string
				json.Unmarshal(c["prompt"], &prompt)
				s, ops := MockPropose(src, prompt)
				l := []any{}
				for _, o := range ops {
					l = append(l, o)
				}
				actual = map[string]any{"summary": s, "ops": l}
			}
		}()
		exp := canon(c["expect"])
		got := canon(actual)
		if reflect.DeepEqual(exp, got) || subsetEq(exp, got) {
			pass++
		} else {
			fail++
			es, _ := json.Marshal(exp)
			gs, _ := json.Marshal(got)
			fmt.Printf("FAIL [%d] %s %s\n   expected: %.600s\n   actual:   %.600s\n", i, kind, file, es, gs)
		}
	}
	fmt.Printf("\nconformance: %d passed, %d failed (of %d cases from the Python reference spec)\n", pass, fail, pass+fail)
	if fail > 0 {
		return 1
	}
	return 0
}
func decodeAny(b []byte) Value {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	dec.Decode(&v)
	return fromJSON(v)
}

// subsetEq: v0.5+ may ADD fields to API objects (e.g. flow run id, attempts); every expected
// key must still be present with an equal value (forward-compatible conformance).
func subsetEq(exp, got any) bool {
	switch e := exp.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range e {
			if !subsetEq(v, g[k]) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(e) {
			return false
		}
		for i := range e {
			if !subsetEq(e[i], g[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(exp, got)
}
