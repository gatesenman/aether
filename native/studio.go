package main

// StudioCore: editor-independent state shared by the native window Studio, the terminal Studio and the agent HTTP API.

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

type ChatMsg struct {
	Me       bool
	Text     string
	Prop     *Proposal
	Decision string // "", accepted, rejected
}

type StudioCore struct {
	mu       sync.Mutex
	Path     string
	Src      string
	Hist     *History
	Diags    []Diag
	Output   []string
	RunErr   string
	Tests    []TestResult
	Chat     []*ChatMsg
	Preview  *App
	Status   string
	Hash     string
	previewV *AppView
}

func NewStudioCore(path string) *StudioCore {
	s := &StudioCore{Path: path}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			s.Src = string(b)
		}
		s.Hist = OpenHistory(path + ".history.json")
	} else {
		s.Hist = OpenHistory("")
	}
	if s.Src == "" {
		s.Src = defaultTodo
	}
	s.Check()
	s.Commit("human", "打开文件（基线）", nil, "baseline", true)
	s.RebuildPreview()
	return s
}

func (s *StudioCore) Check() bool {
	_, s.Diags = CheckSource(s.Src)
	s.Hash = ASTHash(s.Src)
	for _, d := range s.Diags {
		if d["severity"] == "error" {
			return false
		}
	}
	return true
}
func (s *StudioCore) Errors() int {
	n := 0
	for _, d := range s.Diags {
		if d["severity"] == "error" {
			n++
		}
	}
	return n
}
func (s *StudioCore) RebuildPreview() {
	if s.Errors() > 0 {
		return
	}
	app, err := NewApp(s.Src, ":memory:", "")
	if err != nil {
		s.Status = "预览失败: " + err.Error()
		return
	}
	if old := s.Preview; old != nil { // live migration of preview data
		for _, e := range app.It.EntOrder {
			if _, ok := old.It.Entities[e]; ok {
				for _, r := range old.It.Store.All(e) {
					app.It.Store.Insert(e, r)
				}
			}
		}
	}
	s.Preview = app
	if s.previewV != nil {
		s.previewV.App = app
	}
}
func (s *StudioCore) Commit(author, reason string, ops []map[string]any, kind string, semanticOnly bool) {
	if semanticOnly && s.Hist.Head() != nil && s.Hist.Head().Hash == ASTHash(s.Src) {
		return
	}
	s.Hist.Commit(s.Src, author, reason, ops, kind)
}
func (s *StudioCore) Run() {
	it := NewInterp(":memory:", false)
	err := it.Load(s.Src, "")
	s.Output = it.Out
	s.RunErr = ""
	if err != nil {
		if t, ok := err.(*AeThrow); ok {
			s.RunErr = "uncaught: " + show(t.V)
		} else if e, ok := err.(*AeError); ok {
			s.RunErr = "[" + e.Code + "] line " + itoa(e.Line) + ": " + e.Msg
		}
	}
}
func (s *StudioCore) RunTests() {
	it := NewInterp(":memory:", false)
	if err := it.Load(s.Src, ""); err != nil {
		s.Tests = []TestResult{{Name: "加载失败", Message: err.Error()}}
		return
	}
	s.Tests = it.RunTests()
}
func (s *StudioCore) SetSource(src string) {
	s.Src = src
	s.Check()
	s.RebuildPreview()
}
func (s *StudioCore) Save() string {
	if s.Path == "" {
		return "没有文件路径（用 aether studio 文件.ae 打开）"
	}
	s.Commit("human", "保存", nil, "save", true)
	os.WriteFile(s.Path, []byte(s.Src), 0644)
	return "已保存 " + s.Path
}
func (s *StudioCore) Undo() string {
	src, err := s.Hist.Undo()
	if err != nil {
		return "没有可撤销的"
	}
	s.SetSource(src)
	return "已撤销到 #" + s.Hist.Head().Hash
}
func (s *StudioCore) Redo() string {
	src, err := s.Hist.Redo()
	if err != nil {
		return "没有可重做的"
	}
	s.SetSource(src)
	return "已重做到 #" + s.Hist.Head().Hash
}
func (s *StudioCore) Ask(prompt string) *ChatMsg {
	s.Chat = append(s.Chat, &ChatMsg{Me: true, Text: prompt})
	p := Propose(s.Src, prompt)
	m := &ChatMsg{Prop: &p}
	s.Chat = append(s.Chat, m)
	return m
}
func (s *StudioCore) Accept(m *ChatMsg) {
	if m.Prop == nil || m.Decision != "" {
		return
	}
	s.SetSource(m.Prop.Source)
	s.Commit("agent:"+m.Prop.Mode, m.Prop.Prompt+" → "+m.Prop.Summary, m.Prop.Ops, "agent-patch", false)
	m.Decision = "accepted"
}
func (s *StudioCore) Reject(m *ChatMsg) { m.Decision = "rejected" }

func itoa(n int) string { return strconv.Itoa(n) }

// Agent HTTP API (same endpoint names as the Python Studio), so external agents can drive the native Studio.
func (s *StudioCore) Route(method, path string, body []byte, role string) (int, string, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var data map[string]any
	json.Unmarshal(body, &data)
	src, _ := data["source"].(string)
	if src == "" {
		src = s.Src
	}
	if strings.HasSuffix(path, "/_aether/codicon.ttf") {
		return 200, "font/ttf", fontIcon
	}
	if strings.HasPrefix(path, "/preview") {
		if s.Preview == nil {
			return errResp(404, "no preview")
		}
		return s.Preview.Handle(method, strings.TrimPrefix(path, "/preview"), body, role)
	}
	switch path {
	case "/ide/source":
		if v, ok := data["source"].(string); ok && method == "POST" {
			s.SetSource(v)
			s.Commit(strOr(data["author"], "agent:http"), strOr(data["reason"], "set source"), nil, "edit", true)
		}
		return jsonResp(200, map[string]any{"source": s.Src, "hash": s.Hash, "path": s.Path})
	case "/ide/check":
		ast, d := CheckSource(src)
		return jsonResp(200, map[string]any{"diagnostics": diagList(d), "ast": astAny(ast)})
	case "/ide/explain":
		t, err := Explain(src)
		if err != nil {
			return errResp(400, err.Error())
		}
		return jsonResp(200, map[string]any{"text": t, "hash": ASTHash(src)})
	case "/ide/test":
		it := NewInterp(":memory:", false)
		if err := it.Load(src, ""); err != nil {
			return errResp(400, err.Error())
		}
		return jsonResp(200, map[string]any{"results": testsAny(it.RunTests())})
	case "/agent/schema":
		return jsonResp(200, map[string]any{"ops": OpsSchema, "language": LangBrief})
	case "/agent/propose":
		p := Propose(src, strOr(data["prompt"], ""))
		return jsonResp(200, p.JSON())
	case "/agent/apply":
		ops := opsFrom(data["ops"])
		r, err := ApplyOps(src, ops)
		if err != nil {
			return errResp(400, err.Error())
		}
		if apply, _ := data["commit"].(bool); apply && r.OK {
			s.SetSource(r.Source)
			s.Commit(strOr(data["author"], "agent:http"), strOr(data["reason"], "apply ops"), ops, "agent-patch", false)
		}
		return jsonResp(200, map[string]any{"source": r.Source, "diagnostics": diagList(r.Diags), "ok": r.OK, "diff": r.Diff})
	case "/history/log":
		b, _ := json.Marshal(s.Hist)
		return 200, "application/json; charset=utf-8", b
	case "/history/undo":
		return jsonResp(200, map[string]any{"status": s.Undo(), "source": s.Src})
	case "/history/redo":
		return jsonResp(200, map[string]any{"status": s.Redo(), "source": s.Src})
	}
	return errResp(404, "not found")
}
func strOr(v any, d string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return d
}
func opsFrom(v any) []map[string]any {
	out := []map[string]any{}
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}
func astAny(n Node) any {
	if n == nil {
		return nil
	}
	return map[string]any(n)
}
func testsAny(ts []TestResult) []any {
	o := []any{}
	for _, t := range ts {
		o = append(o, map[string]any{"name": t.Name, "ok": t.OK, "message": t.Message, "line": t.Line})
	}
	return o
}
func serveAgentAPI(s *StudioCore, addr string) {
	go http.ListenAndServe(addr, httpHandler(s.Route))
}

const defaultTodo = `module 待办

entity Todo {
  title: Text
  done: Bool = false
  invariant len(title) > 0
}

action toggle(t: Todo) { t.done = not t.done }

view 首页 {
  title "我的待办"
  stat "总数" count(Todo)
  form Todo
  list Todo
}
`

func envLLM() bool { return os.Getenv("AETHER_LLM_API_KEY") != "" }
