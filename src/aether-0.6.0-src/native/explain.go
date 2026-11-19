package main

import (
	"fmt"
	"strings"
)

func Explain(src string) (string, error) {
	prog, err := Parse(src)
	if err != nil {
		return "", err
	}
	D := prog["decls"].([]any)
	by := func(k string) []Node {
		o := []Node{}
		for _, d := range D {
			if d.(Node)["k"] == k {
				o = append(o, d.(Node))
			}
		}
		return o
	}
	mod := "未命名"
	if m := by("Module"); len(m) > 0 {
		mod = m[0]["name"].(string)
	}
	ents, acts, fns, views, flows := by("Entity"), by("Action"), by("Fn"), by("View"), by("Flow")
	tests := append(by("Test"), by("Example")...)
	var out []string
	out = append(out, fmt.Sprintf("# 程序「%s」说明\n", mod))
	out = append(out, fmt.Sprintf("概要：%d 种数据、%d 个动作、%d 个函数、%d 个页面、%d 个流程、%d 个内嵌测试。\n", len(ents), len(acts), len(fns), len(views), len(flows), len(tests)))
	if len(ents) > 0 {
		out = append(out, "## 数据（会自动建表并提供 API）")
		for _, e := range ents {
			fs := []string{}
			for _, f := range e["fields"].([]any) {
				fn := f.(Node)
				s := fn["name"].(string) + "（" + UnparseType(fn["type"])
				if d, ok := fn["default"].(Node); ok {
					s += "，默认 " + Unparse(d)
				}
				fs = append(fs, s+"）")
			}
			out = append(out, "- **"+e["name"].(string)+"**：包含 "+strings.Join(fs, "、")+"。")
			for _, c := range e["invariants"].([]any) {
				out = append(out, "  - 不变式：任何时候都必须满足 `"+Unparse(c.(Node))+"`，否则写入会被拒绝。")
			}
		}
	}
	if len(acts) > 0 {
		out = append(out, "\n## 动作（用户可触发的操作）")
		for _, a := range acts {
			ps := []string{}
			for _, p := range a["params"].([]any) {
				ps = append(ps, p.(Node)["name"].(string)+"（"+UnparseType(p.(Node)["type"])+"）")
			}
			pss := strings.Join(ps, "、")
			if pss == "" {
				pss = "无参数"
			}
			who := "所有用户"
			if al := a["allow"].([]any); len(al) > 0 {
				who = "仅限角色 " + strings.Join(strs(al), " / ")
			}
			out = append(out, "- **"+a["name"].(string)+"**：参数 "+pss+"；"+who+"可执行。"+explainBody(a["body"].(Node)))
			for _, c := range a["requires"].([]any) {
				out = append(out, "  - 前置条件：`"+Unparse(c.(Node))+"`")
			}
			for _, c := range a["ensures"].([]any) {
				out = append(out, "  - 后置保证：`"+Unparse(c.(Node))+"`")
			}
		}
	}
	if len(fns) > 0 {
		out = append(out, "\n## 函数")
		for _, f := range fns {
			ps := []string{}
			for _, p := range f["params"].([]any) {
				ps = append(ps, p.(Node)["name"].(string)+": "+UnparseType(p.(Node)["type"]))
			}
			eff := "，无声明副作用（纯计算）"
			if e := f["effects"].([]any); len(e) > 0 {
				eff = "，副作用：" + strings.Join(strs(e), ", ")
			}
			out = append(out, "- **"+f["name"].(string)+"**("+strings.Join(ps, ", ")+") → "+UnparseType(f["ret"])+eff+"。")
			for _, c := range f["requires"].([]any) {
				out = append(out, "  - 调用前必须满足 `"+Unparse(c.(Node))+"`")
			}
			for _, c := range f["ensures"].([]any) {
				out = append(out, "  - 返回后保证 `"+Unparse(c.(Node))+"`（result 表示返回值）")
			}
		}
	}
	names := map[string]string{"title": "标题", "text": "文字", "stat": "统计", "form": "新增表单", "list": "列表", "table": "表格", "chart": "图表", "button": "按钮", "section": "小节"}
	if len(views) > 0 {
		out = append(out, "\n## 页面")
		for _, v := range views {
			parts := []string{}
			for _, x := range v["items"].([]any) {
				i := x.(Node)
				as := []string{}
				for _, a := range i["args"].([]any) {
					an := a.(Node)
					if an["k"] == "Arrow" {
						as = append(as, "→"+an["target"].(string))
					} else {
						as = append(as, Unparse(an))
					}
				}
				k := i["kind"].(string)
				if nm, ok := names[k]; ok {
					k = nm
				}
				parts = append(parts, k+" "+strings.Join(as, " "))
			}
			out = append(out, "- **"+v["name"].(string)+"**："+strings.Join(parts, "；")+"。")
		}
	}
	if len(flows) > 0 {
		out = append(out, "\n## 流程（按顺序执行，任一步失败即停止并记录）")
		for _, f := range flows {
			ss := []string{}
			for _, s := range f["steps"].([]any) {
				ss = append(ss, s.(Node)["name"].(string))
			}
			out = append(out, "- **"+f["name"].(string)+"**："+strings.Join(ss, " → "))
		}
	}
	if len(tests) > 0 {
		out = append(out, "\n## 内嵌测试与示例（`aether test` 运行）")
		for _, t := range tests {
			if t["k"] == "Test" {
				out = append(out, "- 测试「"+t["name"].(string)+"」")
			} else {
				out = append(out, "- 示例：`"+Unparse(t["expr"].(Node))+"`")
			}
		}
	}
	return strings.Join(out, "\n") + "\n", nil
}
func explainBody(b Node) string {
	acts := []string{}
	for _, s := range b["body"].([]any) {
		st := s.(Node)
		switch st["k"] {
		case "Assign":
			acts = append(acts, "把 "+Unparse(st["target"].(Node))+" 设为 "+Unparse(st["value"].(Node)))
		case "For":
			acts = append(acts, "对 "+Unparse(st["iter"].(Node))+" 中的每一项执行操作")
		case "ExprStmt":
			if st["expr"].(Node)["k"] == "Call" {
				acts = append(acts, "调用 "+Unparse(st["expr"].(Node)))
			}
		}
	}
	if len(acts) == 0 {
		return ""
	}
	return "效果：" + strings.Join(acts, "；") + "。"
}

// Fmt: same algorithm as the reference formatter (indentation by bracket depth).
func Fmt(src string) (string, error) {
	if _, err := Parse(src); err != nil {
		return "", err
	}
	toks, _ := Lex(src)
	by := map[int][]string{}
	for _, t := range toks {
		if t.Kind == "op" && strings.Contains("{}()[]", t.S) && len(t.S) == 1 {
			by[t.Line] = append(by[t.Line], t.S)
		}
	}
	out := []string{}
	depth, blank := 0, 0
	for i, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			blank++
			if blank <= 1 && len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		blank = 0
		br := by[i+1]
		lead := 0
		for _, c := range br {
			if strings.Contains("}])", c) && strings.HasPrefix(line[lead:], c) {
				lead++
			} else {
				break
			}
		}
		ind := depth - lead
		if ind < 0 {
			ind = 0
		}
		out = append(out, strings.Repeat("  ", ind)+line)
		for _, c := range br {
			if strings.Contains("{[(", c) {
				depth++
			} else {
				depth--
			}
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), " \n\t") + "\n", nil
}
