package main

// MVC architecture model (v0.5): Aether programs map 1:1 onto Model / Controller / View.
//   Model      = entity (fields, defaults, invariants, relations)  -> stored tables + CRUD API
//   Controller = action / flow / fn + auth rules                    -> business endpoints & permissions
//   View       = view (+ theme)                                      -> pages
// ArchModel returns that structure for the IDE explorers (aether/model) and `aether mvc`.

import (
	"sort"
	"strings"
)

func typeStr(t Node) string {
	if t == nil {
		return "Any"
	}
	s := t["name"].(string)
	if as, _ := t["args"].([]any); len(as) > 0 {
		ps := []string{}
		for _, a := range as {
			ps = append(ps, typeStr(a.(Node)))
		}
		s += "[" + strings.Join(ps, ", ") + "]"
	}
	if o, _ := t["optional"].(bool); o {
		s += "?"
	}
	return s
}

func walkNames(n any, f func(string)) {
	switch x := n.(type) {
	case Node:
		if x["k"] == "Name" {
			if s, ok := x["name"].(string); ok {
				f(s)
			}
		}
		if x["k"] == "Type" {
			f(x["name"].(string))
		}
		for k, v := range x {
			if k != "span" {
				walkNames(v, f)
			}
		}
	case []any:
		for _, v := range x {
			walkNames(v, f)
		}
	}
}

func ArchModel(src, base string) map[string]any {
	prog, diags := CheckSourceIn(src, base)
	if prog == nil {
		return map[string]any{"error": "parse error", "diagnostics": diagList(diags)}
	}
	ents := map[string]bool{}
	for _, x := range prog["decls"].([]any) {
		if d := x.(Node); d["k"] == "Entity" {
			ents[d["name"].(string)] = true
		}
	}
	lower := map[string]string{}
	for e := range ents {
		lower[strings.ToLower(e)] = e
	}
	model, actions, flows, views, fns := []any{}, []any{}, []any{}, []any{}, []any{}
	relations := []any{}
	endpoints := []any{}
	var auth map[string]any
	var theme any
	touches := func(n any, skip string) []string {
		set := map[string]bool{}
		walkNames(n, func(s string) {
			if ents[s] && s != skip {
				set[s] = true
			}
		})
		o := []string{}
		for k := range set {
			o = append(o, k)
		}
		sort.Strings(o)
		return o
	}
	exprs := func(xs any) []any {
		o := []any{}
		if l, ok := xs.([]any); ok {
			for _, c := range l {
				o = append(o, Unparse(c.(Node)))
			}
		}
		return o
	}
	for _, x := range prog["decls"].([]any) {
		d := x.(Node)
		name, _ := d["name"].(string)
		switch d["k"] {
		case "Entity":
			fields := []any{}
			for _, f := range d["fields"].([]any) {
				fn := f.(Node)
				t := fn["type"].(Node)
				fd := map[string]any{"name": fn["name"], "type": typeStr(t), "line": fn["line"]}
				if fn["default"] != nil {
					fd["default"] = Unparse(fn["default"].(Node))
				}
				target := ""
				if ents[t["name"].(string)] {
					target = t["name"].(string)
				}
				for _, a := range t["args"].([]any) {
					if ents[a.(Node)["name"].(string)] {
						target = a.(Node)["name"].(string)
					}
				}
				fname := fn["name"].(string)
				if target == "" && strings.HasSuffix(fname, "_id") {
					target = lower[strings.ToLower(strings.TrimSuffix(fname, "_id"))]
				}
				if target != "" {
					fd["ref"] = target
					card := "N:1"
					if t["name"] == "List" {
						card = "1:N"
					}
					relations = append(relations, map[string]any{"from": name, "field": fname, "to": target, "cardinality": card})
				}
				fields = append(fields, fd)
			}
			model = append(model, map[string]any{"name": name, "line": d["line"], "fields": fields, "invariants": exprs(d["invariants"])})
			for _, ep := range [][]string{{"GET", "/api/" + name, "列出全部 " + name}, {"POST", "/api/" + name, "新建 " + name}, {"GET", "/api/" + name + "/{id}", "读取一条"}, {"PUT", "/api/" + name + "/{id}", "修改一条"}, {"DELETE", "/api/" + name + "/{id}", "删除一条"}} {
				endpoints = append(endpoints, map[string]any{"method": ep[0], "path": ep[1], "desc": ep[2], "layer": "model", "source": name})
			}
		case "Action":
			ps := []any{}
			for _, p := range d["params"].([]any) {
				pn := p.(Node)
				t, _ := pn["type"].(Node)
				ps = append(ps, map[string]any{"name": pn["name"], "type": typeStr(t)})
			}
			allow, _ := d["allow"].([]any)
			if allow == nil {
				allow = []any{}
			}
			actions = append(actions, map[string]any{"name": name, "line": d["line"], "params": ps, "allow": allow,
				"requires": exprs(d["requires"]), "ensures": exprs(d["ensures"]), "touches": touches(d, ""), "endpoint": "POST /api/action/" + name})
			endpoints = append(endpoints, map[string]any{"method": "POST", "path": "/api/action/" + name, "desc": "执行动作 " + name, "layer": "controller", "source": name, "allow": allow})
		case "Fn":
			fx, _ := d["effects"].([]any)
			fns = append(fns, map[string]any{"name": name, "line": d["line"], "effects": fx, "touches": touches(d, "")})
		case "Flow":
			steps := []any{}
			for _, s := range d["steps"].([]any) {
				sn := s.(Node)
				st := map[string]any{"name": sn["name"], "line": sn["line"], "touches": touches(sn, "")}
				if r, ok := sn["retry"].(int); ok {
					st["retry"], st["backoff"] = r, sn["backoff"]
				}
				steps = append(steps, st)
			}
			flows = append(flows, map[string]any{"name": name, "line": d["line"], "steps": steps, "endpoint": "POST /api/flow/" + name})
			endpoints = append(endpoints, map[string]any{"method": "POST", "path": "/api/flow/" + name, "desc": "运行流程 " + name + "（持久化、可恢复）", "layer": "controller", "source": name})
		case "View":
			uses, acts := map[string]bool{}, map[string]bool{}
			for _, it := range flattenItems(d["items"].([]any)) {
				for _, a := range it.(Node)["args"].([]any) {
					an := a.(Node)
					if an["k"] == "Arrow" {
						acts[an["target"].(string)] = true
					}
					walkNames(an, func(s string) {
						if ents[s] {
							uses[s] = true
						}
					})
				}
			}
			views = append(views, map[string]any{"name": name, "line": d["line"], "entities": sortedKeys(uses), "actions": sortedKeys(acts), "items": len(flattenItems(d["items"].([]any)))})
		case "Auth":
			rules := []any{}
			for _, r := range d["rules"].([]any) {
				rn := r.(Node)
				rules = append(rules, map[string]any{"roles": rn["roles"], "op": rn["op"], "entities": rn["entities"], "line": rn["line"]})
			}
			auth = map[string]any{"roles": d["roles"], "default": d["default"], "signup": d["signup"], "login": d["login"], "rules": rules, "line": d["line"]}
			for _, ep := range [][]string{{"POST", "/api/_auth/signup", "注册"}, {"POST", "/api/_auth/login", "登录，返回令牌"}, {"GET", "/api/_auth/me", "当前用户"}, {"GET", "/api/_auth/users", "用户列表（管理员）"}} {
				endpoints = append(endpoints, map[string]any{"method": ep[0], "path": ep[1], "desc": ep[2], "layer": "controller", "source": "auth"})
			}
		case "Theme":
			theme = Unparse(d["props"].(Node))
		}
	}
	endpoints = append(endpoints, map[string]any{"method": "GET", "path": "/api/_flows/runs", "desc": "流程运行记录", "layer": "controller", "source": "flows"},
		map[string]any{"method": "GET", "path": "/api/_meta", "desc": "界面规格（View 层）", "layer": "view", "source": "views"})
	return map[string]any{
		"model":      map[string]any{"entities": model, "relations": relations},
		"controller": map[string]any{"actions": actions, "flows": flows, "functions": fns, "auth": auth, "endpoints": endpoints},
		"view":       map[string]any{"views": views, "theme": theme},
		"diagnostics": diagList(diags),
	}
}
