package main

import "strings"

func Unparse(e Node) string {
	if e == nil {
		return ""
	}
	sub := func(k string) string { n, _ := e[k].(Node); return Unparse(n) }
	switch e["k"] {
	case "Num":
		return show(e["v"])
	case "Bool":
		if e["v"].(bool) {
			return "true"
		}
		return "false"
	case "Nil":
		return "nil"
	case "Str":
		var b strings.Builder
		b.WriteByte('"')
		for _, p := range e["parts"].([]any) {
			if s, ok := p.(string); ok {
				b.WriteString(s)
			} else {
				b.WriteString("{" + Unparse(p.(Node)) + "}")
			}
		}
		b.WriteByte('"')
		return b.String()
	case "Name":
		return e["name"].(string)
	case "List":
		ps := []string{}
		for _, x := range e["items"].([]any) {
			ps = append(ps, Unparse(x.(Node)))
		}
		return "[" + strings.Join(ps, ", ") + "]"
	case "Record":
		ps := []string{}
		for _, f := range e["fields"].([]any) {
			kv := f.([]any)
			ps = append(ps, kv[0].(string)+": "+Unparse(kv[1].(Node)))
		}
		return "{" + strings.Join(ps, ", ") + "}"
	case "Unary":
		if e["op"] == "-" {
			return "-" + sub("e")
		}
		return "not " + sub("e")
	case "Bin":
		return sub("l") + " " + e["op"].(string) + " " + sub("r")
	case "Pipe":
		return sub("l") + " |> " + sub("r")
	case "Call":
		ps := []string{}
		for _, x := range e["args"].([]any) {
			ps = append(ps, Unparse(x.(Node)))
		}
		return sub("fn") + "(" + strings.Join(ps, ", ") + ")"
	case "Field":
		return sub("obj") + "." + e["name"].(string)
	case "Index":
		return sub("obj") + "[" + sub("idx") + "]"
	case "Lambda":
		ps := []string{}
		for _, p := range e["params"].([]any) {
			ps = append(ps, p.(Node)["name"].(string))
		}
		return "fn(" + strings.Join(ps, ", ") + ") => …"
	case "If":
		return "if " + sub("cond") + " {…}"
	case "Match":
		return "match " + sub("subj") + " {…}"
	case "Spawn":
		return "spawn " + sub("e")
	case "Await":
		return "await " + sub("e")
	}
	return "…"
}
func UnparseType(t any) string {
	n, ok := t.(Node)
	if !ok || n == nil {
		return "Any"
	}
	s := n["name"].(string)
	if args, _ := n["args"].([]any); len(args) > 0 {
		ps := []string{}
		for _, a := range args {
			ps = append(ps, UnparseType(a))
		}
		s += "<" + strings.Join(ps, ", ") + ">"
	}
	if o, _ := n["optional"].(bool); o {
		s += "?"
	}
	return s
}
