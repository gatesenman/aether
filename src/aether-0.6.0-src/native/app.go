package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

//go:embed assets/app.html
var appHTML string

type App struct {
	It  *Interp
	Src string
}

func NewApp(src, db, base string) (*App, error) {
	it := NewInterp(db, false)
	if err := it.Load(src, base); err != nil {
		return nil, err
	}
	it.RecoverFlows()
	return &App{it, src}, nil
}

func (a *App) entitySpec(name string) map[string]any {
	e := a.It.Entities[name]
	fields := []any{}
	for _, f := range e["fields"].([]any) {
		fn := f.(Node)
		fields = append(fields, map[string]any{"name": fn["name"], "type": fn["type"].(Node)["name"]})
	}
	ra := []any{}
	for _, an := range a.It.ActOrder {
		ac := a.It.Actions[an]
		ps := ac["params"].([]any)
		if len(ps) == 1 {
			if t, ok := ps[0].(Node)["type"].(Node); ok && t["name"] == name {
				ra = append(ra, an)
			}
		}
	}
	return map[string]any{"name": name, "fields": fields, "row_actions": ra}
}

// Spec compiles views into a renderer-independent UI spec (same JSON as the Python reference).
func (a *App) Spec() map[string]any {
	it := a.It
	views := []any{}
	src := it.Views
	if len(src) == 0 {
		for _, e := range it.EntOrder {
			src = append(src, Node{"name": e, "items": []any{
				Node{"kind": "list", "args": []any{Node{"k": "Name", "name": e}}},
				Node{"kind": "form", "args": []any{Node{"k": "Name", "name": e}}}}})
		}
	}
	for _, v := range src {
		items := a.compileItems(v["items"].([]any))
		views = append(views, map[string]any{"name": v["name"], "items": items})
	}
	ents := []any{}
	for _, e := range it.EntOrder {
		ents = append(ents, a.entitySpec(e))
	}
	acts := []any{}
	for _, an := range it.ActOrder {
		ac := it.Actions[an]
		ps := []any{}
		for _, p := range ac["params"].([]any) {
			pn := p.(Node)
			t := "Any"
			if tn, ok := pn["type"].(Node); ok {
				t = tn["name"].(string)
			}
			ps = append(ps, map[string]any{"name": pn["name"], "type": t})
		}
		allow, _ := ac["allow"].([]any)
		if allow == nil {
			allow = []any{}
		}
		acts = append(acts, map[string]any{"name": an, "allow": allow, "params": ps})
	}
	flows := []any{}
	for _, f := range it.FlowOrd {
		flows = append(flows, f)
	}
	spec := map[string]any{"module": it.Module, "entities": ents, "flows": flows, "actions": acts, "views": views}
	if it.Auth != nil {
		spec["auth"] = map[string]any{"roles": it.Auth["roles"], "signup": it.Auth["signup"], "login": it.Auth["login"]}
	}
	if it.Theme != nil {
		spec["theme"] = it.eval(it.Theme["props"].(Node), it.Globals)
	}
	return spec
}

func (a *App) compileItems(xs []any) []any {
	it := a.It
	items := []any{}
	for _, x := range xs {
		i := x.(Node)
		kind := i["kind"].(string)
		args := i["args"].([]any)
		el := map[string]any{"kind": kind}
		err := catch(func() {
			arg := func(k int) Node {
				if k >= len(args) {
					panic(aeErr("R099", 0, "missing argument"))
				}
				return args[k].(Node)
			}
			if n := len(args); n > 0 && args[n-1].(Node)["k"] == "Record" && kind != "style" {
				el["style"] = it.eval(args[n-1].(Node), it.Globals)
				args = args[:n-1]
			}
			switch kind {
			case "row", "column", "card", "grid":
				if len(args) > 0 {
					el["title"] = show(it.eval(arg(0), it.Globals))
				}
				ch, _ := i["children"].([]any)
				el["children"] = a.compileItems(ch)
			case "style":
				if len(args) > 0 {
					el["style"] = it.eval(arg(0), it.Globals)
				}
			case "image", "link":
				el["src"] = show(it.eval(arg(0), it.Globals))
				if len(args) > 1 {
					el["text"] = show(it.eval(arg(1), it.Globals))
				}
			case "list", "table", "form", "chart":
				el["entity"] = arg(0)["name"]
				if kind == "chart" {
					if len(args) > 1 {
						el["field"] = arg(1)["name"]
					} else {
						el["field"] = nil
					}
				}
				if kind == "list" && len(args) > 1 {
					el["label_field"] = arg(1)["name"]
				}
			case "button":
				el["label"] = it.eval(arg(0), it.Globals)
				for _, y := range args {
					if y.(Node)["k"] == "Arrow" {
						el["action"] = y.(Node)["target"]
					}
				}
			case "stat":
				el["label"] = it.eval(arg(0), it.Globals)
				el["value"] = show(it.eval(arg(1), it.Globals))
			default:
				if len(args) > 0 {
					el["text"] = show(it.eval(arg(0), it.Globals))
				} else {
					el["text"] = ""
				}
			}
		})
		if err != nil {
			msg := err.Error()
			el = map[string]any{"kind": "error", "text": kind + ": " + msg}
		}
		items = append(items, el)
	}
	return items
}
// OpenAPI keeps the 0.5 shape at /api/_openapi (conformance contract with the Python reference);
// the full 3.1 document with schemas is served at /api/_openapi31 and by `aether openapi`.
func (a *App) OpenAPI() map[string]any {
	paths := map[string]any{}
	for _, e := range a.It.EntOrder {
		paths["/api/"+e] = map[string]any{"get": map[string]any{"summary": "list " + e}, "post": map[string]any{"summary": "create " + e}}
		paths["/api/"+e+"/{id}"] = map[string]any{"get": map[string]any{}, "put": map[string]any{}, "delete": map[string]any{}}
	}
	for _, n := range a.It.ActOrder {
		paths["/api/action/"+n] = map[string]any{"post": map[string]any{"summary": "run action " + n}}
	}
	return map[string]any{"openapi": "3.0.0", "info": map[string]any{"title": a.It.Module, "version": "1"}, "paths": paths}
}

// BuildOpenAPI（0.6 第 2 轮）：带 schema 的 OpenAPI 3.1，借鉴 Encore 从代码生成 API 文档/客户端。
func oaType(it *Interp, t Node) map[string]any {
	n, _ := t["name"].(string)
	var o map[string]any
	switch n {
	case "Int":
		o = map[string]any{"type": "integer"}
	case "Float":
		o = map[string]any{"type": "number"}
	case "Bool":
		o = map[string]any{"type": "boolean"}
	case "Text", "String":
		o = map[string]any{"type": "string"}
	case "List":
		it0 := map[string]any{}
		if as, _ := t["args"].([]any); len(as) > 0 {
			it0 = oaType(it, as[0].(Node))
		}
		o = map[string]any{"type": "array", "items": it0}
	default:
		if _, ok := it.Entities[n]; ok {
			o = map[string]any{"type": "integer", "description": n + " 的 id"}
		} else {
			o = map[string]any{"description": n}
		}
	}
	if t["optional"] == true {
		o["nullable"] = true
	}
	return o
}
func BuildOpenAPI(it *Interp) map[string]any {
	paths := map[string]any{}
	schemas := map[string]any{}
	for _, e := range it.EntOrder {
		props := map[string]any{"id": map[string]any{"type": "integer", "readOnly": true}}
		req := []any{}
		for _, f := range it.Entities[e]["fields"].([]any) {
			fn := f.(Node)
			p := oaType(it, fn["type"].(Node))
			if fn["default"] != nil {
				p["default"] = it.eval(fn["default"].(Node), it.Globals)
			} else {
				req = append(req, fn["name"])
			}
			props[fn["name"].(string)] = p
		}
		sc := map[string]any{"type": "object", "properties": props}
		if len(req) > 0 {
			sc["required"] = req
		}
		if invs, _ := it.Entities[e]["invariants"].([]any); len(invs) > 0 {
			ss := []string{}
			for _, c := range invs {
				ss = append(ss, Unparse(c.(Node)))
			}
			sc["x-aether-invariants"] = ss
		}
		schemas[e] = sc
		ref := map[string]any{"$ref": "#/components/schemas/" + e}
		one := map[string]any{"200": map[string]any{"description": "ok", "content": map[string]any{"application/json": map[string]any{"schema": ref}}}}
		body := map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": ref}}}
		idp := []any{map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "integer"}}}
		paths["/api/"+e] = map[string]any{
			"get":  map[string]any{"summary": "列出 " + e, "responses": map[string]any{"200": map[string]any{"description": "ok", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "array", "items": ref}}}}}},
			"post": map[string]any{"summary": "新建 " + e, "requestBody": body, "responses": one}}
		paths["/api/"+e+"/{id}"] = map[string]any{
			"get":    map[string]any{"summary": "读取 " + e, "parameters": idp, "responses": one},
			"put":    map[string]any{"summary": "更新 " + e, "parameters": idp, "requestBody": body, "responses": one},
			"delete": map[string]any{"summary": "删除 " + e, "parameters": idp, "responses": map[string]any{"200": map[string]any{"description": "ok"}}}}
	}
	for _, n := range it.ActOrder {
		a := it.Actions[n]
		props := map[string]any{}
		req := []any{}
		ps, _ := a["params"].([]any)
		for _, p := range ps {
			pn := p.(Node)
			if t, ok := pn["type"].(Node); ok {
				props[pn["name"].(string)] = oaType(it, t)
			} else {
				props[pn["name"].(string)] = map[string]any{}
			}
			req = append(req, pn["name"])
		}
		op := map[string]any{"summary": "执行 action " + n, "responses": map[string]any{"200": map[string]any{"description": "ok"}, "403": map[string]any{"description": "角色无权限"}, "422": map[string]any{"description": "契约失败（requires/invariant）"}}}
		if len(ps) > 0 {
			op["requestBody"] = map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "properties": props, "required": req}}}}
		}
		if allow, _ := a["allow"].([]any); len(allow) > 0 {
			op["x-aether-allow"] = allow
		}
		paths["/api/action/"+n] = map[string]any{"post": op}
	}
	return map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": it.Module, "version": Version}, "paths": paths, "components": map[string]any{"schemas": schemas}}
}

var reResume = regexp.MustCompile(`^/api/_flows/runs/(\d+)/resume$`)
var reAction = regexp.MustCompile(`^/api/action/(\w+)$`)
var reFlow = regexp.MustCompile(`^/api/flow/([\p{L}\p{N}_]+)$`)
var reEnt = regexp.MustCompile(`^/api/(\w+)(?:/(\d+))?$`)

func jsonResp(code int, v any) (int, string, []byte) {
	return code, "application/json; charset=utf-8", []byte(toJSON(v))
}
func errResp(code int, msg string) (int, string, []byte) {
	return jsonResp(code, map[string]any{"error": msg})
}

func decodeBody(body []byte) *Record {
	if len(bytes.TrimSpace(body)) == 0 {
		return NewRec()
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return NewRec()
	}
	r, ok := fromJSON(v).(*Record)
	if !ok {
		return NewRec()
	}
	return r
}

func (a *App) Handle(method, path string, body []byte, role string) (code int, ctype string, out []byte) {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *PermErr:
				code, ctype, out = errResp(403, e.Msg)
			case *AeThrow:
				code, ctype, out = errResp(400, show(e.V))
			case *AeError:
				code, ctype, out = errResp(400, e.Msg)
			default:
				code, ctype, out = errResp(500, fmt.Sprint(r))
			}
		}
	}()
	if strings.HasSuffix(path, "/_aether/codicon.ttf") {
		return 200, "font/ttf", fontIcon
	}
	data := decodeBody(body)
	it := a.It
	isDev := role == "\x01dev"
	user, role := a.identity(role)
	if strings.HasPrefix(path, "/api/_auth/") {
		return a.handleAuth(method, path, data, user, role)
	}
	if a.authOn() && a.It.Auth["login"] == "required" && user == nil && !isDev && strings.HasPrefix(path, "/api/") && path != "/api/_meta" && path != "/api/_openapi" && path != "/api/_openapi31" {
		return errResp(401, "请先登录")
	}
	if path == "/api/_flows/runs" && method == "GET" {
		return jsonResp(200, it.FlowRuns())
	}
	if m := reResume.FindStringSubmatch(path); m != nil && method == "POST" {
		if a.authOn() && role != a.adminRole() {
			return errResp(403, "只有管理员可以恢复流程")
		}
		id, _ := strconv.ParseInt(m[1], 10, 64)
		return jsonResp(200, it.ResumeFlow(id))
	}
	if path == "/" {
		return 200, "text/html; charset=utf-8", []byte(appHTML)
	}
	if path == "/api/_meta" {
		return jsonResp(200, a.Spec())
	}
	if path == "/api/_openapi" {
		return jsonResp(200, a.OpenAPI())
	}
	if path == "/api/_openapi31" {
		return jsonResp(200, BuildOpenAPI(a.It))
	}
	if m := reAction.FindStringSubmatch(path); m != nil && method == "POST" {
		if _, ok := it.Actions[m[1]]; !ok {
			return errResp(404, "no action "+m[1])
		}
		before := len(it.Out)
		effRole := role
		if a.authOn() && role == a.adminRole() {
			if al, _ := it.Actions[m[1]]["allow"].([]any); len(al) > 0 {
				effRole = al[0].(string) // the administrator may run every action
			}
		}
		if a.authOn() && a.It.Auth["login"] == "required" && user == nil {
			return errResp(401, "请先登录")
		}
		r := it.RunAction(m[1], data, effRole)
		outl := &List{Items: []Value{}}
		for _, s := range it.Out[before:] {
			outl.Items = append(outl.Items, s)
		}
		res := NewRec()
		switch r.(type) {
		case nil, bool, int64, float64, string, *List, *Record:
			res.Set("result", r)
		default:
			res.Set("result", show(r))
		}
		res.Set("output", outl)
		return jsonResp(200, res)
	}
	if m := reFlow.FindStringSubmatch(path); m != nil && method == "POST" {
		return jsonResp(200, it.RunFlow(m[1], data))
	}
	if m := reEnt.FindStringSubmatch(path); m != nil {
		if _, ok := it.Entities[m[1]]; ok {
			e := m[1]
			op := "write"
			if method == "GET" {
				op = "read"
			}
			if !a.entityAllowed(role, e, op) {
				verb := map[string]string{"read": "查看", "write": "修改"}[op]
				return errResp(403, fmt.Sprintf("角色 '%s' 无权%s %s", role, verb, e))
			}
			st := it.Store
			if m[2] == "" {
				if method == "GET" {
					return jsonResp(200, recList(st.All(e)))
				}
				if method == "POST" {
					rec := it.NewRecord(it.Entities[e], data)
					it.CheckInv(e, rec)
					return jsonResp(201, st.Insert(e, rec))
				}
			} else {
				id, _ := strconv.ParseInt(m[2], 10, 64)
				switch method {
				case "GET":
					if r := st.Get(e, id); r != nil {
						return jsonResp(200, r)
					}
					return errResp(404, "not found")
				case "PUT", "PATCH":
					cur := st.Get(e, id)
					if cur == nil {
						cur = NewRec()
					}
					for _, k := range data.Keys {
						cur.Set(k, data.M[k])
					}
					it.CheckInv(e, cur)
					return jsonResp(200, st.Update(e, id, data))
				case "DELETE":
					st.Delete(e, id)
					return jsonResp(200, map[string]any{"deleted": id})
				}
			}
		}
	}
	return errResp(404, "no route "+method+" "+path)
}
func recList(rs []*Record) *List {
	l := &List{Items: []Value{}}
	for _, r := range rs {
		l.Items = append(l.Items, r)
	}
	return l
}

type routeFn func(method, path string, body []byte, role string) (int, string, []byte)

func httpHandler(route routeFn) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		role := r.Header.Get("X-Aether-Role")
		if role == "" {
			role = "user"
		}
		if tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); tok != "" && tok != r.Header.Get("Authorization") {
			role += "\x00" + tok
		}
		code, ct, out := route(r.Method, r.URL.Path, body, role)
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(code)
		w.Write(out)
	})
}
