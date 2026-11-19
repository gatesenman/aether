package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var OpsSchema = map[string]any{
	"add_decl":      map[string]any{"code": "Aether source of one or more declarations"},
	"replace_decl":  map[string]any{"name": "declaration name", "code": "new source"},
	"remove_decl":   map[string]any{"name": "declaration name"},
	"add_field":     map[string]any{"entity": "Entity", "field": "name", "type": "Text|Int|Float|Bool|...", "default": "optional Aether expression"},
	"remove_field":  map[string]any{"entity": "Entity", "field": "name"},
	"rename":        map[string]any{"old": "identifier", "new": "identifier"},
	"add_view_item": map[string]any{"view": "View name", "item": "e.g. list Todo"},
	"set_effects":   map[string]any{"fn": "function name", "effects": []any{"io", "db"}},
}

const LangBrief = "Aether is a declarative+functional language. Declarations: `entity Name { field: Type = default; invariant expr }`, `action name(x: Entity) allow role requires c ensures c { ... }`, `view Name { title|text|stat|form|list|table|chart|button }`, `fn name(a: T) -> T ! effects requires c ensures c { expr }`, `flow N { step \"s\" { } }`, `example expr`, `test \"n\" { }`. v0.5: `import \"lib\" as m` (m.f()), `auth { roles admin, user; default user; signup open; login required; can user read E }`, `theme { primary: \"#2563eb\" }`, view layout `row { card \"T\" { stat ... } }` and trailing style records `title \"x\" { color: \"red\", size: 24 }`, flow steps `step \"s\" retry 3 backoff 200ms { }`. Field access is type-checked against entity fields."

func findDecl(src, name string) Node {
	prog, err := Parse(src)
	if err != nil {
		panic(err)
	}
	for _, d := range prog["decls"].([]any) {
		if d.(Node)["name"] == name {
			return d.(Node)
		}
	}
	panic(aeErr("A001", 0, "no declaration named '%s'", name))
}
func sget(op map[string]any, k string) string { s, _ := op[k].(string); return s }

func applyOp(src string, op map[string]any) string {
	t := sget(op, "op")
	span := func(d Node) (int, int) {
		sp := d["span"].([]any)
		return sp[0].(int), sp[1].(int)
	}
	switch t {
	case "add_decl":
		if _, err := Parse(sget(op, "code")); err != nil {
			panic(err)
		}
		return strings.TrimRight(src, " \n\t") + "\n\n" + strings.TrimSpace(sget(op, "code")) + "\n"
	case "replace_decl", "remove_decl":
		a, b := span(findDecl(src, sget(op, "name")))
		code := ""
		if t == "replace_decl" {
			code = strings.TrimSpace(sget(op, "code"))
		}
		return src[:a] + code + src[b:]
	case "add_field", "add_view_item":
		target := sget(op, "entity")
		line := ""
		if t == "add_field" {
			line = sget(op, "field") + ": " + sget(op, "type")
			if d := sget(op, "default"); d != "" {
				line += " = " + d
			}
		} else {
			target = sget(op, "view")
			line = sget(op, "item")
		}
		a, b := span(findDecl(src, target))
		close := a + strings.LastIndex(src[a:b], "}")
		before := strings.TrimRight(src[:close], " \n\t")
		return before + "\n  " + line + "\n" + src[close:]
	case "remove_field":
		a, b := span(findDecl(src, sget(op, "entity")))
		re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(sget(op, "field")) + `\s*:[^\n]*\n?`)
		body := src[a:b]
		if loc := re.FindStringIndex(body); loc != nil {
			body = body[:loc[0]] + body[loc[1]:]
		}
		return src[:a] + body + src[b:]
	case "rename":
		old, nw := sget(op, "old"), sget(op, "new")
		toks, err := Lex(src)
		if err != nil {
			panic(err)
		}
		var out strings.Builder
		last := 0
		wre := regexp.MustCompile(`(^|[^\p{L}\p{N}_])` + regexp.QuoteMeta(old) + `($|[^\p{L}\p{N}_])`)
		for _, tk := range toks {
			if tk.Kind == "id" && tk.S == old {
				out.WriteString(src[last:tk.Pos])
				out.WriteString(nw)
				last = tk.End
			} else if tk.Kind == "str" && strings.Contains(tk.S, "{") {
				raw := src[tk.Pos:tk.End]
				fixed := interpRe.ReplaceAllStringFunc(raw, func(m string) string {
					for wre.MatchString(m) {
						m = wre.ReplaceAllString(m, "${1}"+nw+"${2}")
					}
					return m
				})
				out.WriteString(src[last:tk.Pos])
				out.WriteString(fixed)
				last = tk.End
			}
		}
		out.WriteString(src[last:])
		return out.String()
	case "set_effects":
		a, b := span(findDecl(src, sget(op, "fn")))
		seg := src[a:b]
		brace := strings.Index(seg, "{")
		head := regexp.MustCompile(`\s*!\s*[\w\s,]+$`).ReplaceAllString(strings.TrimRight(seg[:brace], " \n\t"), "")
		effs := []string{}
		if l, ok := op["effects"].([]any); ok {
			for _, e := range l {
				effs = append(effs, fmt.Sprint(e))
			}
		}
		return src[:a] + head + " ! " + strings.Join(effs, ", ") + " " + seg[brace:] + src[b:]
	}
	panic(aeErr("A002", 0, "unknown op '%s'", t))
}

type OpsResult struct {
	Source string
	Diags  []Diag
	OK     bool
	Diff   string
}

func ApplyOps(src string, ops []map[string]any) (res OpsResult, err error) {
	err = catch(func() {
		nw := src
		for _, op := range ops {
			nw = applyOp(nw, op)
		}
		_, d := CheckSource(nw)
		ok := true
		for _, x := range d {
			if x["severity"] == "error" {
				ok = false
			}
		}
		res = OpsResult{nw, d, ok, UnifiedDiff(src, nw)}
	})
	return
}

// UnifiedDiff: minimal LCS-based unified diff with 3 lines of context.
func UnifiedDiff(a, b string) string {
	if a == b {
		return ""
	}
	A := strings.SplitAfter(a, "\n")
	B := strings.SplitAfter(b, "\n")
	if A[len(A)-1] == "" {
		A = A[:len(A)-1]
	}
	if B[len(B)-1] == "" {
		B = B[:len(B)-1]
	}
	n, m := len(A), len(B)
	L := make([][]int, n+1)
	for i := range L {
		L[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if A[i] == B[j] {
				L[i][j] = L[i+1][j+1] + 1
			} else if L[i+1][j] >= L[i][j+1] {
				L[i][j] = L[i+1][j]
			} else {
				L[i][j] = L[i][j+1]
			}
		}
	}
	type ed struct {
		op   byte
		s    string
		i, j int
	}
	var es []ed
	i, j := 0, 0
	for i < n || j < m {
		if i < n && j < m && A[i] == B[j] {
			es = append(es, ed{' ', A[i], i, j})
			i++
			j++
		} else if j < m && (i == n || L[i][j+1] >= L[i+1][j]) {
			es = append(es, ed{'+', B[j], i, j})
			j++
		} else {
			es = append(es, ed{'-', A[i], i, j})
			i++
		}
	}
	var out strings.Builder
	out.WriteString("--- before.ae\n+++ after.ae\n")
	for k := 0; k < len(es); {
		if es[k].op == ' ' {
			k++
			continue
		}
		s := k - 3
		if s < 0 {
			s = 0
		}
		e := k
		for e < len(es) {
			if es[e].op != ' ' {
				e++
				continue
			}
			run := 0
			for e+run < len(es) && es[e+run].op == ' ' {
				run++
			}
			if run > 6 || e+run == len(es) {
				break
			}
			e += run
		}
		end := e + 3
		if end > len(es) {
			end = len(es)
		}
		ac, bc := 0, 0
		for _, x := range es[s:end] {
			if x.op != '+' {
				ac++
			}
			if x.op != '-' {
				bc++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", es[s].i+1, ac, es[s].j+1, bc)
		for _, x := range es[s:end] {
			line := x.s
			if !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			out.WriteString(string(x.op) + line)
		}
		k = end
	}
	return out.String()
}

var W = `[\p{L}\p{N}_]+`

func MockPropose(src, prompt string) (string, []map[string]any) {
	ast, diags := CheckSource(src)
	ents, views := []string{}, []string{}
	if ast != nil {
		for _, d := range ast["decls"].([]any) {
			dn := d.(Node)
			if dn["k"] == "Entity" {
				ents = append(ents, dn["name"].(string))
			}
			if dn["k"] == "View" {
				views = append(views, dn["name"].(string))
			}
		}
	}
	p := strings.TrimSpace(prompt)
	if m := regexp.MustCompile(`(?i)(?:rename|把)\s*(` + `[A-Za-z_][A-Za-z0-9_]*` + `)\s*(?:to|改名为|改成|重命名为)\s*(` + `[A-Za-z_][A-Za-z0-9_]*` + `)`).FindStringSubmatch(p); m != nil {
		return "重命名 " + m[1] + " → " + m[2], []map[string]any{{"op": "rename", "old": m[1], "new": m[2]}}
	}
	if regexp.MustCompile(`(?i)fix|修复|修正|错误`).MatchString(p) {
		ops := []map[string]any{}
		for _, d := range diags {
			if d["code"] == "W300" {
				fn := regexp.MustCompile(`function '(` + W + `)'`).FindStringSubmatch(d["message"].(string))[1]
				eff := regexp.MustCompile("`! ([^`]+)`").FindStringSubmatch(d["hint"].(string))[1]
				effs := []any{}
				for _, e := range strings.Split(eff, ", ") {
					effs = append(effs, e)
				}
				ops = append(ops, map[string]any{"op": "set_effects", "fn": fn, "effects": effs})
			}
			if d["code"] == "E200" && d["hint"] != nil {
				q := regexp.MustCompile(`'(` + W + `)'`)
				bad := q.FindStringSubmatch(d["message"].(string))[1]
				good := q.FindStringSubmatch(d["hint"].(string))[1]
				ops = append(ops, map[string]any{"op": "rename", "old": bad, "new": good})
			}
		}
		if len(ops) > 0 {
			return fmt.Sprintf("根据诊断自动修复 %d 处", len(ops)), ops
		}
		return "没有可自动修复的问题", ops
	}
	fre := regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*[:：]\s*(Text|Int|Float|Bool|Date)`)
	fields := fre.FindAllStringSubmatch(p, -1)
	target := ""
	for _, e := range ents {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(e) + `\b`).MatchString(p) {
			target = e
			break
		}
	}
	newent := ""
	if m := regexp.MustCompile(`(?:entity|实体|模型|表)\s*([A-Za-z_][A-Za-z0-9_]*)|(?:add|新增|添加|创建)\s+([A-Z][A-Za-z0-9_]*)`).FindStringSubmatch(p); m != nil {
		newent = m[1] + m[2]
	}
	isNew := newent != ""
	for _, e := range ents {
		if e == newent {
			isNew = false
		}
	}
	if isNew {
		var b strings.Builder
		b.WriteString("entity " + newent + " {\n")
		if len(fields) == 0 {
			b.WriteString("  name: Text\n")
		}
		for _, f := range fields {
			b.WriteString("  " + f[1] + ": " + f[2] + "\n")
		}
		b.WriteString("}\n\nview " + newent + "s {\n  title \"" + newent + "\"\n  form " + newent + "\n  list " + newent + "\n}")
		return "新增实体 " + newent + "（自动获得数据表、API 和页面）", []map[string]any{{"op": "add_decl", "code": b.String()}}
	}
	if len(fields) > 0 && (target != "" || len(ents) > 0) {
		e := target
		if e == "" {
			e = ents[0]
		}
		ops := []map[string]any{}
		names := []string{}
		for _, f := range fields {
			ops = append(ops, map[string]any{"op": "add_field", "entity": e, "field": f[1], "type": f[2]})
			names = append(names, f[1])
		}
		return "给 " + e + " 添加字段 " + strings.Join(names, ", "), ops
	}
	if regexp.MustCompile(`(?i)统计|stat|count|数量`).MatchString(p) && len(ents) > 0 && len(views) > 0 {
		e := target
		if e == "" {
			e = ents[0]
		}
		return "在 " + views[0] + " 页面添加 " + e + " 数量统计", []map[string]any{{"op": "add_view_item", "view": views[0], "item": `stat "` + e + ` 总数" count(` + e + `)`}}
	}
	return "Mock 模式未能理解该请求。可尝试：『给 Todo 添加字段 due: Text』『新增实体 Note title: Text』『修复错误』『把 Todo 改名为 Task』『添加统计』；或设置 AETHER_LLM_API_KEY 启用真实大模型。", []map[string]any{}
}

func llmEnabled() bool {
	return os.Getenv("AETHER_LLM_API_KEY") != "" || os.Getenv("AETHER_LLM_BASE_URL") != ""
}

func llmPropose(src, prompt string) (string, []map[string]any, error) {
	return llmProposeWith(src, prompt, nil)
}

// llmProposeWith sends the request; feedback (previous attempt + its diagnostics) enables self-repair.
func llmProposeWith(src, prompt string, feedback []any) (string, []map[string]any, error) {
	key := os.Getenv("AETHER_LLM_API_KEY")
	base := strings.TrimRight(os.Getenv("AETHER_LLM_BASE_URL"), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := os.Getenv("AETHER_LLM_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	_, diags := CheckSource(src)
	sys := LangBrief + "\nYou edit programs ONLY via JSON operations. Schema: " + toJSON(OpsSchema) + "\nReply with JSON only: {\"summary\": \"中文说明\", \"ops\": [...]}"
	user := "Program:\n```\n" + src + "\n```\nDiagnostics: " + toJSON(diagList(diags)) + "\nRequest: " + prompt
	msgs := []any{map[string]any{"role": "system", "content": sys}, map[string]any{"role": "user", "content": user}}
	msgs = append(msgs, feedback...)
	body, _ := json.Marshal(map[string]any{"model": model, "temperature": 0.2, "messages": msgs})
	req, _ := http.NewRequest("POST", base+"/chat/completions", bytes.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")
	cl := http.Client{Timeout: 60 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("LLM HTTP %d: %.200s", resp.StatusCode, raw)
	}
	var r struct {
		Choices []struct{ Message struct{ Content string } }
	}
	if err := json.Unmarshal(raw, &r); err != nil || len(r.Choices) == 0 {
		return "", nil, fmt.Errorf("bad LLM response: %.200s", raw)
	}
	txt := r.Choices[0].Message.Content
	i, j := strings.Index(txt, "{"), strings.LastIndex(txt, "}")
	if i < 0 || j < i {
		return "", nil, fmt.Errorf("no JSON in LLM reply")
	}
	var out struct {
		Summary string
		Ops     []map[string]any
	}
	if err := json.Unmarshal([]byte(txt[i:j+1]), &out); err != nil {
		return "", nil, err
	}
	return out.Summary, out.Ops, nil
}
func diagList(d []Diag) []any {
	o := []any{}
	for _, x := range d {
		o = append(o, map[string]any(x))
	}
	return o
}

type Proposal struct {
	Mode, Summary, Prompt string
	Ops                   []map[string]any
	OpsResult
}

func Propose(src, prompt string) Proposal {
	mode := "mock"
	var summary string
	var ops []map[string]any
	if llmEnabled() {
		mode = "llm"
		var feedback []any
		var lastErr error
		for round := 0; round < 3; round++ {
			s, o, err := llmProposeWith(src, prompt, feedback)
			if err != nil {
				lastErr = err
				break
			}
			summary, ops, lastErr = s, o, nil
			res, aerr := ApplyOps(src, o)
			if aerr == nil && res.OK {
				if round > 0 {
					summary += fmt.Sprintf("（经过 %d 轮自我修复）", round)
				}
				break
			}
			problem := ""
			if aerr != nil {
				problem = aerr.Error()
			} else {
				problem = toJSON(diagList(res.Diags))
			}
			prev, _ := json.Marshal(map[string]any{"summary": s, "ops": o})
			feedback = append(feedback, map[string]any{"role": "assistant", "content": string(prev)},
				map[string]any{"role": "user", "content": "Your ops did not type-check: " + problem + "\nFix them and reply with the full corrected JSON only."})
		}
		if lastErr != nil {
			mode = "mock"
			summary, ops = MockPropose(src, prompt)
			summary = "[LLM 调用失败，已回退到 mock: " + lastErr.Error() + "] " + summary
		}
	} else {
		summary, ops = MockPropose(src, prompt)
	}
	res, err := ApplyOps(src, ops)
	if err != nil {
		d := Diag{"severity": "error", "code": "A000", "message": err.Error(), "line": 0, "col": 1}
		if e, ok := err.(*AeError); ok {
			d = e.JSON()
		}
		res = OpsResult{src, []Diag{d}, false, ""}
	}
	return Proposal{mode, summary, prompt, ops, res}
}
func (p Proposal) JSON() map[string]any {
	ops := []any{}
	for _, o := range p.Ops {
		ops = append(ops, o)
	}
	return map[string]any{"mode": p.Mode, "summary": p.Summary, "ops": ops, "source": p.Source, "diagnostics": diagList(p.Diags), "ok": p.OK, "diff": p.Diff}
}
