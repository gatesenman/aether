package main

// Native Aether Studio: standard IDE layout (title bar with command center, activity bar, side bar with
// explorer/outline/search/history/testing/run, editor tabs + breadcrumbs, preview editor group, bottom panel
// with Problems/Output/Terminal/Contracts/Explain, right-side agent panel, status bar, command palette).
// All language intelligence comes from the in-process Aether LSP server (lsp.go). Icons are Codicons glyphs.

import (
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ---- theme (VS Code "Dark Modern"-like, Aether accent) ----
var (
	tTitle   = rgb(0x181818)
	tAct     = rgb(0x181818)
	tSide    = rgb(0x181818)
	tEditor  = rgb(0x1f1f1f)
	tBorder  = rgb(0x2b2b2b)
	tTabIn   = rgb(0x181818)
	tInk     = rgb(0xcccccc)
	tInkHi   = rgb(0xffffff)
	tMuted   = rgb(0x9d9d9d)
	tDim     = rgb(0x6e7681)
	tAccent  = rgb(0x6d5ef5)
	tAccentB = rgb(0x8b7ff7)
	tSelRow  = rgb(0x2a2d2e)
	tSelAct  = rgb(0x37373d)
	tInput   = rgb(0x313131)
	tCurLine = rgb(0x282828)
	tErr     = rgb(0xf14c4c)
	tWarn    = rgb(0xcca700)
	tInfo    = rgb(0x3794ff)
	tOK      = rgb(0x73c991)
	tWidget  = rgb(0x252526)
)
var semColor = map[string]colorT{
	"keyword": rgb(0xc586c0), "string": rgb(0xce9178), "number": rgb(0xb5cea8), "comment": rgb(0x6a9955),
	"function": rgb(0xdcdcaa), "method": rgb(0xdcdcaa), "event": rgb(0xdcdcaa), "class": rgb(0x4ec9b0), "type": rgb(0x4ec9b0),
	"enum": rgb(0x4ec9b0), "namespace": rgb(0x4ec9b0), "enumMember": rgb(0x4fc1ff), "variable": rgb(0x9cdcfe),
	"parameter": rgb(0x9cdcfe), "property": rgb(0x9cdcfe), "operator": rgb(0xd4d4d4),
}

type lDiag struct {
	Range    LRange `json:"range"`
	Severity int    `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	raw      map[string]any
}
type semSpan struct {
	line, col, n int
	typ          string
}
type popItem struct {
	Label, Detail, Icon string
	Run                 func()
}
type popup struct {
	Title, Input string
	Items        []popItem
	Sel          int
	Filter       func(q string) []popItem
	OnEnter      func(q string) // free text (rename, agent prompt)
}

type GuiStudio struct {
	mu                                  sync.Mutex
	S                                   *StudioCore
	cores                               map[string]*StudioCore
	L                                   *LSPServer
	URI                                 string
	ver                                 int
	Ed                                  Editor
	UI                                  *UI
	AV                                  *AppView
	Root                                string
	Tabs                                []string
	Activity                            string
	SideOpen                            bool
	PanelOpen                           bool
	AgentOpen                           bool
	PrevOpen                            bool
	PanelTab                            string
	Focus                               string // editor|prompt|preview|terminal|search
	Prompt                              string
	Term                                string
	TermLog                             []string
	Search                              string
	Refs                                [][2]LPos
	Pal                                 *popup
	Comp                                *popup
	Hover                               string
	Win                                 Window
	mouseX, mouseY, hoverLine, hoverCol int
	moveAt                              time.Time
	hoverMouse, hoverDone               bool
	Diags                               []lDiag
	Sem                                 []semSpan
	Symbols                             []map[string]any
	Notice                              string
	lastEdit                            time.Time
	ChatTop                             int
	PanelTop                            int
	SideTop                             int
	outLog                              []string
}

func runGuiStudio(path string, win Window, script string) {
	g := newGuiStudio(path)
	g.Win = win
	if runGuiStudioAPI != "" && runGuiStudioAPI != "off" {
		go http.ListenAndServe(runGuiStudioAPI, httpHandler(func(m, p string, b []byte, role string) (int, string, []byte) {
			g.mu.Lock()
			defer g.mu.Unlock()
			code, ct, out := g.S.Route(m, p, b, role)
			if strings.HasPrefix(p, "/ide/source") || strings.HasPrefix(p, "/agent/apply") || strings.HasPrefix(p, "/history/") {
				g.Ed.Set(g.S.Src)
				g.lspChange()
			}
			return code, ct, out
		}))
	}
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	w, h := win.Size()
	g.UI.C = NewCanvas(w, h)
	redraw := func() {
		ww, hh := win.Size()
		if g.UI.C.Img.Rect.Dx() != ww || g.UI.C.Img.Rect.Dy() != hh {
			g.UI.C = NewCanvas(ww, hh)
		}
		g.Draw()
		win.Present(g.UI.C.Img)
	}
	g.mu.Lock()
	redraw()
	if script != "" {
		runScript(g, script, redraw)
	}
	g.mu.Unlock()
	for {
		select {
		case ev := <-win.Events():
			g.mu.Lock()
			if ev.Type == EvClose {
				g.mu.Unlock()
				return
			}
			switch ev.Type {
			case EvKey:
				g.hoverMouse = false
				g.Key(ev)
			case EvMouse:
				g.Mouse(ev)
			case EvMove:
				if g.mouseMove(ev.X, ev.Y) {
					redraw()
				}
				g.mu.Unlock()
				continue
			}
			redraw()
			g.mu.Unlock()
		case <-tick.C:
			g.mu.Lock()
			if g.Ed.Dirty && time.Since(g.lastEdit) > 400*time.Millisecond {
				g.sync()
				redraw()
			}
			if g.hoverTick() {
				redraw()
			}
			g.mu.Unlock()
		}
	}
}

func newGuiStudio(path string) *GuiStudio {
	g := &GuiStudio{cores: map[string]*StudioCore{}, UI: &UI{Vals: map[string]string{}}, Activity: "explorer", SideOpen: true, PanelOpen: true, AgentOpen: true, PrevOpen: true, PanelTab: "problems", Focus: "editor"}
	g.L = NewLSP(nil)
	g.L.OnNotify = func(method string, params any) {
		if method == "textDocument/publishDiagnostics" {
			m := params.(map[string]any)
			b, _ := json.Marshal(m["diagnostics"])
			var ds []lDiag
			json.Unmarshal(b, &ds)
			var raws []map[string]any
			json.Unmarshal(b, &raws)
			for i := range ds {
				ds[i].raw = raws[i]
			}
			if m["uri"] == g.URI {
				g.Diags = ds
			}
		}
	}
	g.L.HistFor = func(uri string) *History {
		if c, ok := g.cores[uriPath(uri)]; ok {
			return c.Hist
		}
		return g.S.Hist
	}
	g.lsp("initialize", map[string]any{"capabilities": map[string]any{}})
	g.AV = &AppView{}
	if path != "" {
		abs, _ := filepath.Abs(path)
		path = abs
		g.Root = filepath.Dir(abs)
	} else {
		g.Root, _ = os.Getwd()
	}
	g.open(path)
	return g
}

// lsp sends one request to the in-process language server and decodes the JSON result.
func (g *GuiStudio) lsp(method string, params map[string]any) any {
	if params == nil {
		params = map[string]any{}
	}
	if _, ok := params["textDocument"]; !ok && g.URI != "" {
		params["textDocument"] = map[string]any{"uri": g.URI}
	}
	b, _ := json.Marshal(params)
	res, err := g.L.Handle(method, b)
	if err != nil {
		g.Notice = err.Error()
		return nil
	}
	rb, _ := json.Marshal(res)
	var out any
	json.Unmarshal(rb, &out)
	return out
}
func (g *GuiStudio) open(path string) {
	if g.S != nil && g.Ed.Dirty {
		g.sync()
	}
	c, ok := g.cores[path]
	if !ok {
		c = NewStudioCore(path)
		g.cores[path] = c
	}
	g.S = c
	c.previewV = g.AV
	g.AV.App = c.Preview
	if path == "" {
		g.URI = "untitled:示例.ae"
	} else {
		g.URI = "file://" + path
	}
	found := false
	for _, t := range g.Tabs {
		if t == path {
			found = true
		}
	}
	if !found {
		g.Tabs = append(g.Tabs, path)
	}
	g.Ed = Editor{Anchor: -1}
	g.Ed.Set(c.Src)
	g.ver++
	g.lsp("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": g.URI, "languageId": "aether", "version": g.ver, "text": c.Src}})
	g.refreshLang()
}
func (g *GuiStudio) lspChange() {
	g.ver++
	g.lsp("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": g.URI, "version": g.ver}, "contentChanges": []any{map[string]any{"text": g.Ed.String()}}})
	g.refreshLang()
}
func (g *GuiStudio) refreshLang() {
	g.Sem = g.Sem[:0]
	legend := semTypes
	if r, ok := g.lsp("textDocument/semanticTokens/full", nil).(map[string]any); ok {
		data, _ := r["data"].([]any)
		line, col := 0, 0
		for i := 0; i+4 < len(data); i += 5 {
			dl, dc, n, ty := int(data[i].(float64)), int(data[i+1].(float64)), int(data[i+2].(float64)), int(data[i+3].(float64))
			if dl > 0 {
				line, col = line+dl, dc
			} else {
				col += dc
			}
			g.Sem = append(g.Sem, semSpan{line, col, n, legend[ty]})
		}
	}
	g.Symbols = g.Symbols[:0]
	if l, ok := g.lsp("textDocument/documentSymbol", nil).([]any); ok {
		for _, x := range l {
			g.Symbols = append(g.Symbols, x.(map[string]any))
		}
	}
}

// sync pushes the editor buffer into the core (preview/history) and the LSP (diagnostics/semantics).
func (g *GuiStudio) sync() {
	if g.Ed.Dirty || g.Ed.String() != g.S.Src {
		g.Ed.Dirty = false
		g.S.SetSource(g.Ed.String())
		if g.S.Errors() == 0 {
			g.S.Commit("human", "人工编辑", nil, "edit", true)
		}
		g.AV.App = g.S.Preview
		g.lspChange()
	}
}
func (g *GuiStudio) setSource(src string) {
	g.Ed.Set(src)
	g.Ed.Dirty = true
	g.sync()
}

// applyEdit applies an LSP WorkspaceEdit to the current buffer.
func (g *GuiStudio) applyEdit(we any) {
	m, _ := we.(map[string]any)
	ch, _ := m["changes"].(map[string]any)
	edits, _ := ch[g.URI].([]any)
	text := g.Ed.String()
	type ed struct {
		a, b int
		t    string
	}
	var es []ed
	for _, e := range edits {
		em := e.(map[string]any)
		rb, _ := json.Marshal(em["range"])
		var r LRange
		json.Unmarshal(rb, &r)
		es = append(es, ed{PosToOff(text, r.Start), PosToOff(text, r.End), em["newText"].(string)})
	}
	sort.Slice(es, func(i, j int) bool { return es[i].a > es[j].a })
	for _, e := range es {
		text = text[:e.a] + e.t + text[e.b:]
	}
	cur := g.Ed.Cur
	g.setSource(text)
	g.Ed.Cur = min(cur, len(g.Ed.Buf))
}
func (g *GuiStudio) caretPos() LPos {
	l, c := g.Ed.LineCol()
	line := g.Ed.Lines()[l]
	return LPos{l, utf16Len(string([]rune(line)[:c]))}
}
func (g *GuiStudio) gotoPos(p LPos) {
	line := g.Ed.Lines()
	if p.Line < len(line) {
		rs := []rune(line[p.Line])
		c, u := 0, 0
		for c < len(rs) && u < p.Character {
			u += utf16Len(string(rs[c]))
			c++
		}
		g.Ed.SetLineCol(p.Line, c)
	}
	g.Focus = "editor"
}
func (g *GuiStudio) errCount() (int, int) {
	e, w := 0, 0
	for _, d := range g.Diags {
		if d.Severity == 1 {
			e++
		} else if d.Severity == 2 {
			w++
		}
	}
	return e, w
}
func (g *GuiStudio) log(lines ...string) {
	g.outLog = append(g.outLog, lines...)
}

// ---------- commands ----------
type command struct{ id, label, key, icon string }

var commands = []command{
	{"save", "文件: 保存", "Ctrl+S", "save"},
	{"run", "Aether: 运行程序", "Ctrl+R", "play"},
	{"test", "Aether: 运行测试", "Ctrl+T", "beaker"},
	{"format", "格式化文档", "Ctrl+Shift+I", "list-flat"},
	{"explain", "Aether: 解释程序（自然语言投影）", "", "book"},
	{"contracts", "Aether: 显示契约", "", "shield"},
	{"preview", "Aether: 切换应用预览", "Ctrl+Shift+V", "open-preview"},
	{"undo", "历史: 撤销（语义）", "Ctrl+Z", "discard"},
	{"redo", "历史: 重做", "Ctrl+Y", "redo"},
	{"def", "转到定义", "F12", "go-to-file"},
	{"refs", "查找所有引用", "Shift+F12", "references"},
	{"rename", "重命名符号", "F2", "edit"},
	{"fix", "快速修复 / 智能体补丁", "Ctrl+.", "lightbulb"},
	{"complete", "触发建议", "Ctrl+Space", "symbol-keyword"},
	{"hover", "显示悬停信息", "Ctrl+I", "info"},
	{"symbol", "转到文件中的符号", "Ctrl+Shift+O", "symbol-class"},
	{"side", "视图: 切换主侧边栏", "Ctrl+B", "layout-sidebar-left"},
	{"panel", "视图: 切换面板", "Ctrl+J", "layout-panel"},
	{"agent", "视图: 切换智能体面板", "Ctrl+Shift+A", "layout-sidebar-right"},
	{"ask", "智能体: 描述修改…", "", "sparkle"},
	{"terminal", "终端: 聚焦终端", "Ctrl+`", "terminal"},
	{"problems", "视图: 问题", "Ctrl+Shift+M", "warning"},
}

func (g *GuiStudio) exec(id string) {
	s := g.S
	switch id {
	case "save":
		g.sync()
		s.Status = s.Save()
		g.lsp("textDocument/didSave", nil)
		g.Notice = s.Status
	case "run":
		g.sync()
		r, _ := g.lsp("aether/run", nil).(map[string]any)
		g.log("$ aether run " + filepath.Base(s.Path))
		for _, o := range r["output"].([]any) {
			g.log(fmt.Sprint(o))
		}
		if e := fmt.Sprint(r["error"]); e != "" {
			g.log("错误: " + e)
		}
		g.PanelOpen, g.PanelTab = true, "output"
	case "test":
		g.sync()
		s.RunTests()
		g.Activity, g.SideOpen = "testing", true
		pass := 0
		for _, t := range s.Tests {
			if t.OK {
				pass++
			}
		}
		g.log(fmt.Sprintf("$ aether test  —  %d 通过, %d 失败", pass, len(s.Tests)-pass))
	case "format":
		if e, ok := g.lsp("textDocument/formatting", map[string]any{"options": map[string]any{"tabSize": 2, "insertSpaces": true}}).([]any); ok && len(e) > 0 {
			g.applyEdit(map[string]any{"changes": map[string]any{g.URI: e}})
		}
	case "explain":
		g.PanelOpen, g.PanelTab = true, "explain"
	case "contracts":
		g.PanelOpen, g.PanelTab = true, "contracts"
	case "preview":
		g.PrevOpen = !g.PrevOpen
	case "undo":
		g.Notice = s.Undo()
		g.Ed.Set(s.Src)
		g.lspChange()
		g.Activity, g.SideOpen = "history", true
	case "redo":
		g.Notice = s.Redo()
		g.Ed.Set(s.Src)
		g.lspChange()
		g.Activity, g.SideOpen = "history", true
	case "def":
		g.sync()
		if r, ok := g.lsp("textDocument/definition", map[string]any{"position": g.caretPos()}).(map[string]any); ok {
			rb, _ := json.Marshal(r["range"])
			var rr LRange
			json.Unmarshal(rb, &rr)
			g.gotoPos(rr.Start)
		}
	case "refs":
		g.sync()
		g.Refs = nil
		if l, ok := g.lsp("textDocument/references", map[string]any{"position": g.caretPos(), "context": map[string]any{"includeDeclaration": true}}).([]any); ok {
			for _, x := range l {
				rb, _ := json.Marshal(x.(map[string]any)["range"])
				var rr LRange
				json.Unmarshal(rb, &rr)
				g.Refs = append(g.Refs, [2]LPos{rr.Start, rr.End})
			}
		}
		g.Activity, g.SideOpen = "search", true
	case "rename":
		g.sync()
		pr, ok := g.lsp("textDocument/prepareRename", map[string]any{"position": g.caretPos()}).(map[string]any)
		if !ok {
			return
		}
		pos := g.caretPos()
		g.Pal = &popup{Title: "重命名 '" + fmt.Sprint(pr["placeholder"]) + "' 为（回车确认）", Input: fmt.Sprint(pr["placeholder"]), OnEnter: func(q string) {
			if we := g.lsp("textDocument/rename", map[string]any{"position": pos, "newName": q}); we != nil {
				g.applyEdit(we)
				g.Notice = "已重命名为 " + q
			}
		}}
	case "fix":
		g.sync()
		g.codeActions()
	case "complete":
		g.sync()
		g.completion()
	case "hover":
		g.sync()
		g.Hover = ""
		if r, ok := g.lsp("textDocument/hover", map[string]any{"position": g.caretPos()}).(map[string]any); ok {
			g.Hover = fmt.Sprint(r["contents"].(map[string]any)["value"])
		}
	case "symbol":
		g.symbolPalette()
	case "side":
		g.SideOpen = !g.SideOpen
	case "panel":
		g.PanelOpen = !g.PanelOpen
	case "agent":
		g.AgentOpen = !g.AgentOpen
	case "ask":
		g.AgentOpen, g.Focus = true, "prompt"
	case "terminal":
		g.PanelOpen, g.PanelTab, g.Focus = true, "terminal", "terminal"
	case "problems":
		g.PanelOpen, g.PanelTab = true, "problems"
	}
}

func (g *GuiStudio) commandPalette() {
	g.Pal = &popup{Title: "", Filter: func(q string) []popItem {
		var out []popItem
		if strings.HasPrefix(q, "@") {
			return g.symbolItems(strings.TrimPrefix(q, "@"))
		}
		for _, c := range commands {
			if q == "" || strings.Contains(strings.ToLower(c.label), strings.ToLower(strings.TrimPrefix(q, ">"))) {
				id := c.id
				out = append(out, popItem{c.label, c.key, c.icon, func() { g.exec(id) }})
			}
		}
		if r, ok := g.lsp("aether/flows", nil).(map[string]any); ok {
			for _, f := range r["flows"].([]any) {
				fn := fmt.Sprint(f)
				lbl := "Aether: 运行流程 " + fn
				if q == "" || strings.Contains(lbl, strings.TrimPrefix(q, ">")) {
					out = append(out, popItem{lbl, "", "play-circle", func() { g.runFlow(fn) }})
				}
			}
		}
		return out
	}}
}
func (g *GuiStudio) symbolItems(q string) []popItem {
	var out []popItem
	icon := map[float64]string{2: "symbol-namespace", 23: "symbol-structure", 12: "symbol-method", 6: "symbol-event", 5: "symbol-class", 24: "symbol-event", 10: "symbol-enum", 8: "symbol-field", 13: "symbol-variable"}
	var walk func(l []any, depth int)
	walk = func(l []any, depth int) {
		for _, x := range l {
			m := x.(map[string]any)
			nm := fmt.Sprint(m["name"])
			if q == "" || strings.Contains(nm, q) {
				rb, _ := json.Marshal(m["selectionRange"])
				var rr LRange
				json.Unmarshal(rb, &rr)
				out = append(out, popItem{strings.Repeat("  ", depth) + nm, fmt.Sprint(m["detail"]), icon[m["kind"].(float64)], func() { g.gotoPos(rr.Start) }})
			}
			if ch, ok := m["children"].([]any); ok {
				walk(ch, depth+1)
			}
		}
	}
	l := []any{}
	for _, s := range g.Symbols {
		l = append(l, s)
	}
	walk(l, 0)
	return out
}
func (g *GuiStudio) symbolPalette() {
	g.Pal = &popup{Input: "@", Filter: func(q string) []popItem { return g.symbolItems(strings.TrimPrefix(q, "@")) }}
}
func (g *GuiStudio) codeActions() {
	l, _ := g.Ed.LineCol()
	var diags []any
	for _, d := range g.Diags {
		if d.Range.Start.Line <= l && d.Range.End.Line >= l {
			diags = append(diags, d.raw)
		}
	}
	p := g.caretPos()
	acts, _ := g.lsp("textDocument/codeAction", map[string]any{"range": map[string]any{"start": p, "end": p}, "context": map[string]any{"diagnostics": diags}}).([]any)
	var items []popItem
	for _, a := range acts {
		am := a.(map[string]any)
		icon := "lightbulb"
		if strings.HasPrefix(fmt.Sprint(am["title"]), "智能体") {
			icon = "sparkle"
		}
		items = append(items, popItem{fmt.Sprint(am["title"]), fmt.Sprint(am["kind"]), icon, func() {
			if e, ok := am["edit"]; ok {
				g.applyEdit(e)
			}
			if c, ok := am["command"].(map[string]any); ok {
				g.lsp("workspace/executeCommand", map[string]any{"command": c["command"], "arguments": c["arguments"]})
			}
			g.Notice = "已应用：" + fmt.Sprint(am["title"])
		}})
	}
	if len(items) == 0 {
		items = append(items, popItem{"没有可用的代码操作", "", "info", func() {}})
	}
	g.Comp = &popup{Title: "代码操作", Items: items}
}
func (g *GuiStudio) completion() {
	r, _ := g.lsp("textDocument/completion", map[string]any{"position": g.caretPos()}).(map[string]any)
	items, _ := r["items"].([]any)
	l, c := g.Ed.LineCol()
	line := []rune(g.Ed.Lines()[l])
	k := c
	for k > 0 && isIdRune(line[k-1]) {
		k--
	}
	prefix := string(line[k:c])
	kindIcon := map[float64]string{2: "symbol-method", 3: "symbol-method", 5: "symbol-field", 6: "symbol-variable", 7: "symbol-class", 9: "symbol-namespace", 13: "symbol-enum", 14: "symbol-keyword", 15: "symbol-snippet", 20: "symbol-enum-member", 22: "symbol-structure", 23: "symbol-event"}
	var out []popItem
	for _, it := range items {
		m := it.(map[string]any)
		lbl := fmt.Sprint(m["label"])
		filt := lbl
		if f, ok := m["filterText"].(string); ok {
			filt = f
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(filt), strings.ToLower(prefix)) {
			continue
		}
		ins := filt
		if t, ok := m["insertText"].(string); ok {
			ins = snippetPlain(t)
		}
		out = append(out, popItem{lbl, fmt.Sprint(m["detail"]), kindIcon[m["kind"].(float64)], func() {
			g.Ed.Cur -= c - k
			g.Ed.Buf = append(g.Ed.Buf[:g.Ed.Cur], g.Ed.Buf[g.Ed.Cur+(c-k):]...)
			g.Ed.Insert(ins)
			g.lastEdit = time.Now()
		}})
		if len(out) >= 60 {
			break
		}
	}
	if len(out) > 0 {
		g.Comp = &popup{Title: "建议", Items: out}
	}
}
func isIdRune(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r >= 0x4e00 && r <= 0x9fff
}
func snippetPlain(s string) string {
	out := []rune{}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '$' && i+1 < len(rs) {
			if rs[i+1] == '{' {
				j := i + 2
				for j < len(rs) && rs[j] != ':' && rs[j] != '}' {
					j++
				}
				if j < len(rs) && rs[j] == ':' {
					k := j + 1
					for k < len(rs) && rs[k] != '}' {
						k++
					}
					out = append(out, rs[j+1:k]...)
					i = k
					continue
				}
				i = j
				continue
			}
			if rs[i+1] >= '0' && rs[i+1] <= '9' {
				i++
				continue
			}
		}
		out = append(out, rs[i])
	}
	return string(out)
}
func (g *GuiStudio) runFlow(name string) {
	g.sync()
	res := g.AV.call(g.UI, "POST", "/api/flow/"+name, "{}")
	g.log("$ aether flow " + name)
	if st, ok := res["steps"].(*List); ok {
		for _, x := range st.Items {
			r := x.(*Record)
			mark := "成功"
			if !truthy(r.M["ok"]) {
				mark = "失败"
			}
			g.log(fmt.Sprintf("  步骤 %s  %s  %s", show(r.M["step"]), mark, show(r.M["value"])))
		}
	}
	g.PanelOpen, g.PanelTab = true, "output"
}
func (g *GuiStudio) terminal(cmd string) {
	g.TermLog = append(g.TermLog, "aether> "+cmd)
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return
	}
	out := func(s ...string) { g.TermLog = append(g.TermLog, s...) }
	switch f[0] {
	case "help":
		out("命令: run | test | check | explain | fmt | flow <名称> | history | undo | redo | hash | clear")
	case "clear":
		g.TermLog = nil
	case "run":
		g.sync()
		r, _ := g.lsp("aether/run", nil).(map[string]any)
		for _, o := range r["output"].([]any) {
			out(fmt.Sprint(o))
		}
		if e := fmt.Sprint(r["error"]); e != "" {
			out("错误: " + e)
		}
	case "test":
		g.sync()
		r, _ := g.lsp("aether/tests", nil).(map[string]any)
		for _, x := range r["results"].([]any) {
			m := x.(map[string]any)
			st := "PASS"
			if m["ok"] != true {
				st = "FAIL"
			}
			out(fmt.Sprintf("%s  %v  %v", st, m["name"], m["message"]))
		}
	case "check":
		e, w := g.errCount()
		out(fmt.Sprintf("%d 个错误, %d 个警告", e, w))
		for _, d := range g.Diags {
			out(fmt.Sprintf("  %d:%d %s %s", d.Range.Start.Line+1, d.Range.Start.Character+1, d.Code, d.Message))
		}
	case "explain":
		r, _ := g.lsp("aether/explain", nil).(map[string]any)
		out(strings.Split(fmt.Sprint(r["text"]), "\n")...)
	case "fmt":
		g.exec("format")
		out("已格式化")
	case "flow":
		if len(f) > 1 {
			g.runFlow(f[1])
			out(g.outLog[max(0, len(g.outLog)-4):]...)
		}
	case "history":
		for _, e := range g.S.Hist.Entries {
			out(fmt.Sprintf("#%d %s %s %s %s", e.ID, e.Hash, e.Author, e.Kind, e.Reason))
		}
	case "undo", "redo":
		g.exec(f[0])
		out(g.Notice)
	case "hash":
		out(g.S.Hash)
	default:
		out("未知命令: " + f[0] + "（输入 help）")
	}
}

// ---------- input ----------
func editLine(s *string, ev Event) {
	if ev.Key == KBackspace {
		if r := []rune(*s); len(r) > 0 {
			*s = string(r[:len(r)-1])
		}
	} else if ev.Rune != 0 && !ev.Ctrl {
		*s += string(ev.Rune)
	}
}
func (g *GuiStudio) popKey(p *popup, ev Event, isPal bool) bool {
	items := p.Items
	if p.Filter != nil {
		items = p.Filter(p.Input)
	}
	switch {
	case ev.Key == KEsc:
		return false
	case ev.Key == KDown:
		p.Sel = min(p.Sel+1, max(0, len(items)-1))
	case ev.Key == KUp:
		p.Sel = max(0, p.Sel-1)
	case ev.Key == KEnter || ev.Key == KTab:
		if p.OnEnter != nil {
			p.OnEnter(p.Input)
		} else if p.Sel < len(items) {
			items[p.Sel].Run()
		}
		return false
	default:
		if isPal {
			editLine(&p.Input, ev)
			p.Sel = 0
		} else if ev.Rune != 0 || ev.Key == KBackspace {
			return false
		}
	}
	return true
}

func (g *GuiStudio) Key(ev Event) {
	if g.Pal != nil {
		pal := g.Pal
		if !g.popKey(pal, ev, true) && g.Pal == pal {
			g.Pal = nil
		}
		return
	}
	if g.Comp != nil {
		comp := g.Comp
		if g.popKey(comp, ev, false) {
			return
		}
		if g.Comp == comp {
			g.Comp = nil
		}
		if ev.Key == KEsc || ev.Key == KEnter || ev.Key == KTab {
			return
		}
	}
	if ev.Key == KEsc {
		g.Hover = ""
		return
	}
	switch ev.Key {
	case KF12:
		if ev.Shift {
			g.exec("refs")
		} else {
			g.exec("def")
		}
		return
	case KF2:
		g.exec("rename")
		return
	case KF1:
		g.commandPalette()
		return
	case KF5:
		g.exec("run")
		return
	}
	if ev.Ctrl {
		r := ev.Rune
		if ev.Shift {
			switch r {
			case 'P', 'p':
				g.commandPalette()
			case 'O', 'o':
				g.symbolPalette()
			case 'A', 'a':
				g.exec("agent")
			case 'V', 'v':
				g.exec("preview")
			case 'M', 'm':
				g.exec("problems")
			case 'I', 'i':
				g.exec("format")
			case 'F', 'f':
				g.Activity, g.SideOpen, g.Focus = "search", true, "search"
			case 'E', 'e':
				g.Activity, g.SideOpen = "explorer", true
			}
			return
		}
		switch r {
		case 'c', 'x':
			if g.Focus == "editor" || g.Focus == "" {
				txt := g.Ed.Selected()
				if txt == "" { // like VS Code: no selection = whole line
					a, b := g.Ed.LineBounds()
					txt = string(g.Ed.Buf[a:b])
					if r == 'x' {
						g.Ed.Anchor, g.Ed.Cur = a, b
					}
				}
				g.clipSet(txt)
				if r == 'x' {
					g.Ed.DeleteSel()
					g.lastEdit = time.Now()
				}
				g.Notice = fmt.Sprintf("已复制 %d 个字符", len([]rune(txt)))
			}
			return
		case 'v':
			txt := g.clipGet()
			switch g.Focus {
			case "prompt":
				g.Prompt += strings.ReplaceAll(txt, "\n", " ")
			case "terminal":
				g.Term += strings.ReplaceAll(txt, "\n", " ")
			case "search":
				g.Search += strings.ReplaceAll(txt, "\n", " ")
			case "preview":
				if u := g.UI; u.Focus != "" {
					u.Vals[u.Focus] += strings.ReplaceAll(txt, "\n", " ")
				}
			default:
				g.Ed.Insert(txt)
				g.lastEdit = time.Now()
			}
			return
		case 'a':
			if g.Focus == "editor" || g.Focus == "" {
				g.Ed.Anchor, g.Ed.Cur = 0, len(g.Ed.Buf)
			}
			return
		case 's':
			g.exec("save")
		case 'z':
			g.exec("undo")
		case 'y':
			g.exec("redo")
		case 'r':
			g.exec("run")
		case 't':
			g.exec("test")
		case 'l':
			g.exec("format")
		case 'b':
			g.exec("side")
		case 'j':
			g.exec("panel")
		case ' ':
			g.exec("complete")
		case '.':
			g.exec("fix")
		case 'i':
			g.exec("hover")
		case '`':
			g.exec("terminal")
		case 'p':
			g.Pal = &popup{Filter: func(q string) []popItem {
				var out []popItem
				for _, f := range g.listFiles() {
					if strings.HasSuffix(f, ".ae") && strings.Contains(f, q) {
						ff := f
						out = append(out, popItem{filepath.Base(f), filepath.Dir(f), "file-code", func() { g.open(ff) }})
					}
				}
				return out
			}}
		}
		return
	}
	switch g.Focus {
	case "prompt":
		if ev.Key == KEnter {
			g.ask(g.Prompt)
		} else {
			editLine(&g.Prompt, ev)
		}
	case "terminal":
		if ev.Key == KEnter {
			g.terminal(g.Term)
			g.Term = ""
		} else {
			editLine(&g.Term, ev)
		}
	case "search":
		editLine(&g.Search, ev)
	case "preview":
		u := g.UI
		if u.Focus != "" {
			v := u.Vals[u.Focus]
			editLine(&v, ev)
			u.Vals[u.Focus] = v
		}
	default:
		g.Hover = ""
		if g.Ed.Key(ev) && g.Ed.Dirty {
			g.lastEdit = time.Now()
			if ev.Rune == '.' {
				g.sync()
				g.completion()
			}
		}
	}
}
func (g *GuiStudio) ask(p string) {
	if strings.TrimSpace(p) == "" {
		return
	}
	g.sync()
	g.S.Ask(p)
	g.Prompt = ""
	g.ChatTop = 1 << 20
	g.AgentOpen = true
}

// ---------- layout ----------
type lay struct {
	W, H                                                                   int
	title, act, side, edTabs, crumbs, ed, pvTabs, pv, panel, agent, status image.Rectangle
}

func (g *GuiStudio) layout() lay {
	r := g.UI.C.Img.Rect
	L := lay{W: r.Dx(), H: r.Dy()}
	L.title = image.Rect(0, 0, L.W, 32)
	L.status = image.Rect(0, L.H-22, L.W, L.H)
	L.act = image.Rect(0, 32, 46, L.H-22)
	x := 46
	if g.SideOpen {
		L.side = image.Rect(46, 32, 46+250, L.H-22)
		x = L.side.Max.X + 1
	}
	right := L.W
	if g.AgentOpen {
		L.agent = image.Rect(L.W-330, 32, L.W, L.H-22)
		right = L.agent.Min.X - 1
	}
	bottom := L.H - 22
	if g.PanelOpen {
		L.panel = image.Rect(x, bottom-int(float64(L.H)*0.28), right, bottom)
		bottom = L.panel.Min.Y - 1
	}
	edR := right
	if g.PrevOpen {
		edR = x + (right-x)*56/100
		L.pvTabs = image.Rect(edR+1, 32, right, 32+35)
		L.pv = image.Rect(edR+1, 32+35, right, bottom)
	}
	L.edTabs = image.Rect(x, 32, edR, 32+35)
	L.crumbs = image.Rect(x, 67, edR, 67+22)
	L.ed = image.Rect(x, 89, edR, bottom)
	return L
}

func (g *GuiStudio) Mouse(ev Event) {
	L := g.layout()
	p := image.Point{ev.X, ev.Y}
	if ev.Button == 4 || ev.Button == 5 {
		d := 3
		if ev.Button == 4 {
			d = -3
		}
		switch {
		case p.In(L.ed):
			g.Ed.Top = max(0, g.Ed.Top+d)
		case p.In(L.pv):
			g.AV.Scroll = max(0, g.AV.Scroll+d*20)
		case p.In(L.panel):
			g.PanelTop = max(0, g.PanelTop+d*20)
		case p.In(L.agent):
			g.ChatTop = max(0, g.ChatTop+d*20)
		case p.In(L.side):
			g.SideTop = max(0, g.SideTop+d*20)
		}
		return
	}
	if g.Pal != nil || g.Comp != nil {
		if g.UI.Click(ev.X, ev.Y) {
			return
		}
		g.Pal, g.Comp = nil, nil
		return
	}
	g.Hover = ""
	if p.In(L.pv) {
		g.Focus = "preview"
	}
	if g.UI.Click(ev.X, ev.Y) {
		return
	}
	if p.In(L.ed) {
		g.Focus = "editor"
		lh := 20
		line := g.Ed.Top + (ev.Y-L.ed.Min.Y-4)/lh
		lines := g.Ed.Lines()
		if line >= 0 && line < len(lines) {
			x0 := L.ed.Min.X + 64
			col := 0
			rs := []rune(lines[line])
			for col < len(rs) && x0+Measure(string(rs[:col+1]), 14, true) <= ev.X+4 {
				col++
			}
			g.Ed.SetLineCol(line, col)
		}
	}
}

// ---------- drawing ----------
func (g *GuiStudio) Draw() {
	u := g.UI
	c := u.C
	u.Hits = u.Hits[:0]
	L := g.layout()
	c.Fill(c.Img.Rect, tBorder)
	g.drawTitle(L)
	g.drawActivity(L)
	if g.SideOpen {
		g.drawSide(L.side)
	}
	g.drawEditor(L)
	if g.PrevOpen {
		g.drawPreview(L)
	}
	if g.PanelOpen {
		g.drawPanel(L.panel)
	}
	if g.AgentOpen {
		g.drawAgent(L.agent)
	}
	g.drawStatus(L)
	if g.Comp != nil {
		g.drawCompletion(L)
	}
	if g.Pal != nil {
		g.drawPalette(L)
	}
}

func (g *GuiStudio) iconBtn(x, y int, icon string, col colorT, clip image.Rectangle, f func()) {
	g.UI.C.Icon(x, y, icon, 16, col, clip)
	g.UI.Hits = append(g.UI.Hits, Hit{image.Rect(x-4, y-4, x+20, y+20), f})
}

func (g *GuiStudio) drawTitle(L lay) {
	c := g.UI.C
	r := L.title
	c.Fill(r, tTitle)
	c.Fill(image.Rect(0, r.Max.Y-1, L.W, r.Max.Y), tBorder)
	c.Icon(12, 8, "symbol-misc", 16, tAccentB, r)
	x := 36
	for _, m := range []string{"文件", "编辑", "选择", "查看", "运行", "终端", "帮助"} {
		x += c.Text(x, 8, m, 13, false, tInk, r) + 14
		if m == "查看" {
			g.UI.Hits = append(g.UI.Hits, Hit{image.Rect(x-40, 0, x, 32), func() { g.commandPalette() }})
		}
	}
	// command center
	name := filepath.Base(g.Root)
	cw := 420
	cx := L.W/2 - cw/2
	cc := image.Rect(cx, 5, cx+cw, 27)
	c.RoundRect(cc, 6, rgb(0x2a2a2a))
	c.Border(cc, rgb(0x3c3c3c))
	lbl := name + "  —  命令面板  Ctrl+Shift+P"
	tw := MeasureIconText(lbl, 12)
	c.IconText(cx+(cw-tw)/2, 9, "search", lbl, 12, false, tMuted, cc)
	g.UI.Hits = append(g.UI.Hits, Hit{cc, func() { g.commandPalette() }})
	// layout toggles
	rx := L.W - 100
	for _, t := range [][2]string{{"layout-sidebar-left", "side"}, {"layout-panel", "panel"}, {"layout-sidebar-right", "agent"}} {
		id := t[1]
		on := map[string]bool{"side": g.SideOpen, "panel": g.PanelOpen, "agent": g.AgentOpen}[id]
		col := tMuted
		if on {
			col = tInk
		}
		g.iconBtn(rx, 8, t[0], col, r, func() { g.exec(id) })
		rx += 28
	}
}

func (g *GuiStudio) drawActivity(L lay) {
	c := g.UI.C
	r := L.act
	c.Fill(r, tAct)
	y := r.Min.Y + 10
	for _, a := range [][3]string{{"explorer", "files", "资源管理器"}, {"search", "search", "搜索"}, {"history", "source-control", "历史"}, {"testing", "beaker", "测试"}, {"run", "debug-alt", "运行与流程"}, {"agent", "robot", "智能体"}} {
		id := a[0]
		on := g.Activity == id && g.SideOpen
		if id == "agent" {
			on = g.AgentOpen
		}
		col := rgb(0x868686)
		if on {
			col = tInkHi
			c.Fill(image.Rect(r.Min.X, y-8, r.Min.X+2, y+32), tAccentB)
		}
		c.Icon(r.Min.X+11, y, a[1], 24, col, r)
		g.UI.Hits = append(g.UI.Hits, Hit{image.Rect(r.Min.X, y-8, r.Max.X, y+32), func() {
			if id == "agent" {
				g.AgentOpen = !g.AgentOpen
				return
			}
			if g.Activity == id && g.SideOpen {
				g.SideOpen = false
			} else {
				g.Activity, g.SideOpen = id, true
			}
			if id == "testing" {
				g.sync()
				g.S.RunTests()
			}
		}})
		y += 48
	}
	c.Icon(r.Min.X+11, r.Max.Y-40, "settings-gear", 24, rgb(0x868686), r)
	c.Icon(r.Min.X+11, r.Max.Y-84, "account", 24, rgb(0x868686), r)
}

func (g *GuiStudio) listFiles() []string {
	var out []string
	ents, _ := os.ReadDir(g.Root)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, filepath.Join(g.Root, e.Name()))
	}
	return out
}

func (g *GuiStudio) sideHeader(r image.Rectangle, title string) int {
	c := g.UI.C
	c.Text(r.Min.X+20, r.Min.Y+10, title, 11, false, tMuted, r)
	c.Icon(r.Max.X-28, r.Min.Y+9, "ellipsis", 16, tMuted, r)
	return r.Min.Y + 36
}
func (g *GuiStudio) section(r image.Rectangle, y int, title string) int {
	c := g.UI.C
	c.Icon(r.Min.X+4, y+3, "chevron-down", 16, tInk, r)
	c.Text(r.Min.X+22, y+4, title, 11, false, tInk, r)
	return y + 24
}
func (g *GuiStudio) row(r image.Rectangle, y, indent int, icon string, icol colorT, label, detail string, sel bool, f func()) int {
	c := g.UI.C
	rr := image.Rect(r.Min.X, y, r.Max.X, y+22)
	if sel {
		c.Fill(rr.Intersect(r), tSelAct)
	}
	x := r.Min.X + 8 + indent*12
	if icon != "" {
		c.Icon(x, y+3, icon, 16, icol, rr.Intersect(r))
		x += 22
	}
	x += c.Text(x, y+3, label, 13, false, tInk, rr.Intersect(r))
	if detail != "" {
		c.Text(x+8, y+4, detail, 12, false, tDim, rr.Intersect(r))
	}
	if f != nil && rr.Overlaps(r) {
		g.UI.Hits = append(g.UI.Hits, Hit{rr.Intersect(r), f})
	}
	return y + 22
}

func (g *GuiStudio) drawSide(r image.Rectangle) {
	c := g.UI.C
	s := g.S
	c.Fill(r, tSide)
	switch g.Activity {
	case "explorer":
		y := g.sideHeader(r, "资源管理器")
		y = g.section(r, y, strings.ToUpper(filepath.Base(g.Root)))
		for _, f := range g.listFiles() {
			st, _ := os.Stat(f)
			ff := f
			icon, col := "file-text", tMuted
			var fn func()
			if st != nil && st.IsDir() {
				icon, col = "chevron-right", tInk
			} else if strings.HasSuffix(f, ".ae") {
				icon, col = "file-code", tAccentB
				fn = func() { g.open(ff) }
			}
			detail := ""
			if strings.HasSuffix(f, ".aedb") {
				icon, col, detail = "database", rgb(0xd7ba7d), "AeStore"
			} else if strings.HasSuffix(f, ".history.json") {
				icon, col, detail = "history", rgb(0xd7ba7d), "历史"
			}
			y = g.row(r, y, 1, icon, col, filepath.Base(f), detail, f == s.Path, fn)
			if y > r.Min.Y+r.Dy()/2 {
				break
			}
		}
		y += 8
		y = g.section(r, max(y, r.Min.Y+r.Dy()/2), "大纲")
		for _, it := range g.symbolItems("") {
			lbl := strings.TrimLeft(it.Label, " ")
			ind := (len(it.Label) - len(lbl)) / 2
			y = g.row(r, y, 1+ind, it.Icon, rgb(0x75beff), lbl, it.Detail, false, it.Run)
		}
	case "search":
		y := g.sideHeader(r, "搜索")
		box := image.Rect(r.Min.X+12, y, r.Max.X-12, y+26)
		col := rgb(0x3c3c3c)
		if g.Focus == "search" {
			col = tAccentB
		}
		c.Fill(box, tInput)
		c.Border(box, col)
		ph := g.Search
		pc := tInk
		if ph == "" {
			ph, pc = "搜索", tDim
		}
		c.Text(box.Min.X+8, box.Min.Y+5, ph, 13, false, pc, box)
		g.UI.Hits = append(g.UI.Hits, Hit{box, func() { g.Focus = "search" }})
		y += 36
		if g.Search != "" {
			for i, ln := range g.Ed.Lines() {
				if k := strings.Index(ln, g.Search); k >= 0 {
					li, ci := i, len([]rune(ln[:k]))
					y = g.row(r, y, 1, "search", tMuted, strings.TrimSpace(ln), fmt.Sprintf("行 %d", i+1), false, func() { g.Ed.SetLineCol(li, ci); g.Focus = "editor" })
				}
			}
		}
		if len(g.Refs) > 0 {
			y = g.section(r, y+6, fmt.Sprintf("引用（%d）", len(g.Refs)))
			lines := g.Ed.Lines()
			for _, rf := range g.Refs {
				p := rf[0]
				txt := ""
				if p.Line < len(lines) {
					txt = strings.TrimSpace(lines[p.Line])
				}
				y = g.row(r, y, 1, "references", tInfo, txt, fmt.Sprintf("%d:%d", p.Line+1, p.Character+1), false, func() { g.gotoPos(p) })
			}
		}
	case "history":
		y := g.sideHeader(r, "历史（内容寻址 · 语义撤销）")
		bx := r.Min.X + 14
		bx += g.UI.Button(bx, y, "$discard 撤销", tInput, tInk, r, func() { g.exec("undo") }) + 6
		g.UI.Button(bx, y, "$redo 重做", tInput, tInk, r, func() { g.exec("redo") })
		y += 36
		y = g.section(r, y, "时间线")
		for i := len(s.Hist.Entries) - 1; i >= 0; i-- {
			e := s.Hist.Entries[i]
			icon, col := "account", tInk
			if strings.HasPrefix(e.Author, "agent") {
				icon, col = "robot", tAccentB
			}
			if i == s.Hist.Pos {
				icon, col = "circle-filled", tOK
			}
			y = g.row(r, y, 1, icon, col, e.Reason, fmt.Sprintf("#%d %s", e.ID, e.Hash[:6]), i == s.Hist.Pos, nil)
			c.Text(r.Min.X+42, y, e.Author+" · "+e.Kind+" · "+e.Time[11:], 11, false, tDim, r)
			y += 18
		}
	case "testing":
		y := g.sideHeader(r, "测试")
		g.UI.Button(r.Min.X+14, y, "$run-all 运行全部测试", tAccent, tInkHi, r, func() { g.exec("test") })
		y += 38
		pass := 0
		for _, t := range s.Tests {
			if t.OK {
				pass++
			}
		}
		y = g.section(r, y, fmt.Sprintf("%s（%d 通过 / %d 失败）", filepath.Base(s.Path), pass, len(s.Tests)-pass))
		for _, t := range s.Tests {
			icon, col := "pass", tOK
			if !t.OK {
				icon, col = "error", tErr
			}
			ln := t.Line
			y = g.row(r, y, 1, icon, col, t.Name, t.Message, false, func() { g.Ed.SetLineCol(ln-1, 0); g.Focus = "editor" })
		}
	case "run":
		y := g.sideHeader(r, "运行与流程")
		g.UI.Button(r.Min.X+14, y, "$play 运行程序", tAccent, tInkHi, r, func() { g.exec("run") })
		y += 38
		y = g.section(r, y, "流程（flow）")
		if fr, ok := g.lsp("aether/flows", nil).(map[string]any); ok {
			for _, f := range fr["flows"].([]any) {
				fn := fmt.Sprint(f)
				y = g.row(r, y, 1, "play-circle", tOK, fn, "点击运行", false, func() { g.runFlow(fn) })
			}
		}
		y = g.section(r, y+6, "应用")
		y = g.row(r, y, 1, "open-preview", tInfo, "切换应用预览", "Ctrl+Shift+V", false, func() { g.exec("preview") })
		y = g.row(r, y, 1, "server", tInfo, "智能体 API", runGuiStudioAPI, false, nil)
	}
}

func (g *GuiStudio) drawEditor(L lay) {
	c := g.UI.C
	s := g.S
	// tabs
	c.Fill(L.edTabs, tTabIn)
	x := L.edTabs.Min.X
	for _, t := range g.Tabs {
		name := filepath.Base(t)
		if t == "" {
			name = "示例.ae"
		}
		w := MeasureIconText(name, 13) + 46
		tr := image.Rect(x, L.edTabs.Min.Y, x+w, L.edTabs.Max.Y)
		active := t == s.Path
		if active {
			c.Fill(tr, tEditor)
			c.Fill(image.Rect(tr.Min.X, tr.Min.Y, tr.Max.X, tr.Min.Y+2), tAccentB)
		}
		col := tMuted
		if active {
			col = tInkHi
		}
		c.IconText(x+12, tr.Min.Y+10, "file-code", name, 13, false, col, tr)
		ci := "close"
		if active && g.Ed.Dirty {
			ci = "circle-filled"
		}
		c.Icon(tr.Max.X-26, tr.Min.Y+10, ci, 16, col, tr)
		tt := t
		g.UI.Hits = append(g.UI.Hits, Hit{tr, func() { g.open(tt) }})
		c.Fill(image.Rect(tr.Max.X, tr.Min.Y, tr.Max.X+1, tr.Max.Y), tBorder)
		x += w + 1
	}
	// editor actions
	ax := L.edTabs.Max.X - 84
	g.iconBtn(ax, L.edTabs.Min.Y+10, "play", tInk, L.edTabs, func() { g.exec("run") })
	g.iconBtn(ax+28, L.edTabs.Min.Y+10, "open-preview", tInk, L.edTabs, func() { g.exec("preview") })
	g.iconBtn(ax+56, L.edTabs.Min.Y+10, "ellipsis", tInk, L.edTabs, func() { g.commandPalette() })
	// breadcrumbs
	c.Fill(L.crumbs, tEditor)
	crumb := []string{filepath.Base(g.Root), filepath.Base(s.Path)}
	cl, _ := g.Ed.LineCol()
	for _, sy := range g.Symbols {
		rb, _ := json.Marshal(sy["range"])
		var rr LRange
		json.Unmarshal(rb, &rr)
		if cl >= rr.Start.Line && cl <= rr.End.Line {
			crumb = append(crumb, fmt.Sprint(sy["name"]))
			if ch, ok := sy["children"].([]any); ok {
				for _, cx := range ch {
					cm := cx.(map[string]any)
					rb, _ := json.Marshal(cm["range"])
					var r2 LRange
					json.Unmarshal(rb, &r2)
					if cl >= r2.Start.Line && cl <= r2.End.Line {
						crumb = append(crumb, fmt.Sprint(cm["name"]))
					}
				}
			}
		}
	}
	bx := L.crumbs.Min.X + 14
	for i, cr := range crumb {
		if i > 0 {
			c.Icon(bx, L.crumbs.Min.Y+3, "chevron-right", 16, tDim, L.crumbs)
			bx += 18
		}
		bx += c.Text(bx, L.crumbs.Min.Y+3, cr, 12, false, tMuted, L.crumbs) + 4
	}
	// body
	r := L.ed
	c.Fill(r, tEditor)
	lh := 20
	vis := (r.Dy() - 8) / lh
	if g.Focus == "editor" {
		g.Ed.Scroll(vis)
	}
	lines := g.Ed.Lines()
	cl, cc := g.Ed.LineCol()
	sevAt := map[int]int{}
	for _, d := range g.Diags {
		for l := d.Range.Start.Line; l <= d.Range.End.Line; l++ {
			if sevAt[l] == 0 || d.Severity < sevAt[l] {
				sevAt[l] = d.Severity
			}
		}
	}
	semBy := map[int][]semSpan{}
	for _, sp := range g.Sem {
		semBy[sp.line] = append(semBy[sp.line], sp)
	}
	code := image.Rect(r.Min.X+60, r.Min.Y, r.Max.X, r.Max.Y)
	for i := 0; i < vis && g.Ed.Top+i < len(lines); i++ {
		ln := g.Ed.Top + i
		y := r.Min.Y + 4 + i*lh
		if ln == cl {
			c.Fill(image.Rect(r.Min.X, y, r.Max.X, y+lh), tCurLine)
		}
		ncol := rgb(0x6e7681)
		if ln == cl {
			ncol = tInk
		}
		ns := fmt.Sprint(ln + 1)
		c.Text(r.Min.X+44-Measure(ns, 12, true), y+3, ns, 12, true, ncol, r)
		if sv := sevAt[ln]; sv > 0 {
			icon, col := "error", tErr
			if sv == 2 {
				icon, col = "warning", tWarn
			} else if sv >= 3 {
				icon, col = "info", tInfo
			}
			c.Icon(r.Min.X+4, y+2, icon, 14, col, r)
		}
		if sa, sb, ok := g.Ed.SelRange(); ok {
			ls := lineStart(lines, ln)
			le2 := ls + len([]rune(lines[ln]))
			if sb > ls && sa <= le2 {
				rs := []rune(lines[ln])
				a, b := max(sa, ls)-ls, min(sb, le2)-ls
				x0 := code.Min.X + 4 + Measure(string(rs[:a]), 14, true)
				x1 := code.Min.X + 4 + Measure(string(rs[:b]), 14, true)
				if sb > le2 {
					x1 += 7
				}
				c.Fill(image.Rect(x0, y, x1, y+lh), rgb(0x264f78))
			}
		}
		g.drawCodeLine(code.Min.X+4, y+1, ln, lines[ln], semBy[ln], code)
		// squiggles
		for _, d := range g.Diags {
			if d.Range.Start.Line > ln || d.Range.End.Line < ln {
				continue
			}
			rs := []rune(lines[ln])
			a, b := 0, len(rs)
			if d.Range.Start.Line == ln {
				a = u16ToRune(rs, d.Range.Start.Character)
			}
			if d.Range.End.Line == ln {
				b = u16ToRune(rs, d.Range.End.Character)
			}
			x0 := code.Min.X + 4 + Measure(string(rs[:a]), 14, true)
			x1 := code.Min.X + 4 + Measure(string(rs[:max(a, b)]), 14, true)
			col := map[int]colorT{1: tErr, 2: tWarn}[d.Severity]
			if col == (colorT{}) {
				col = tInfo
			}
			for xx := x0; xx < max(x1, x0+6); xx++ {
				yy := y + lh - 3 + []int{0, 1, 2, 1}[(xx-x0)%4]/1 - 1
				c.Fill(image.Rect(xx, yy, xx+1, yy+1).Intersect(code), col)
			}
		}
		if ln == cl && g.Focus == "editor" {
			cx := code.Min.X + 4 + Measure(string([]rune(lines[ln])[:cc]), 14, true)
			c.Fill(image.Rect(cx, y+1, cx+2, y+lh-1), rgb(0xaeafad))
		}
	}
	// minimap-like overview ruler
	if n := len(lines); n > 0 {
		c.Fill(image.Rect(r.Max.X-12, r.Min.Y, r.Max.X-11, r.Max.Y), tBorder)
		for _, d := range g.Diags {
			yy := r.Min.Y + d.Range.Start.Line*r.Dy()/n
			col := map[int]colorT{1: tErr, 2: tWarn}[d.Severity]
			if col == (colorT{}) {
				col = tInfo
			}
			c.Fill(image.Rect(r.Max.X-10, yy, r.Max.X-2, yy+3), col)
		}
		ty := r.Min.Y + g.Ed.Top*r.Dy()/n
		c.Fill(image.Rect(r.Max.X-10, ty, r.Max.X-2, min(r.Max.Y, ty+max(10, vis*r.Dy()/n))), rgb(0x3a3a3a))
	}
	// hover widget
	if g.Hover != "" {
		hl, hc := cl, cc
		if g.hoverMouse && g.hoverLine < len(lines) {
			hl, hc = g.hoverLine, min(g.hoverCol, len([]rune(lines[g.hoverLine])))
		}
		hx := code.Min.X + 4 + Measure(string([]rune(lines[hl])[:hc]), 14, true)
		hy := r.Min.Y + 4 + (hl-g.Ed.Top+1)*lh + 2
		g.drawMarkdownBox(hx, hy, 460, g.Hover, r)
	}
}
func lineStart(lines []string, ln int) int {
	o := 0
	for i := 0; i < ln && i < len(lines); i++ {
		o += len([]rune(lines[i])) + 1
	}
	return o
}
func u16ToRune(rs []rune, u int) int {
	c, n := 0, 0
	for c < len(rs) && n < u {
		n += utf16Len(string(rs[c]))
		c++
	}
	return c
}
func (g *GuiStudio) drawCodeLine(x, y, ln int, line string, spans []semSpan, clip image.Rectangle) {
	c := g.UI.C
	rs := []rune(line)
	colors := make([]colorT, len(rs))
	for i := range colors {
		colors[i] = rgb(0xd4d4d4)
	}
	for _, sp := range spans {
		a := u16ToRune(rs, sp.col)
		b := u16ToRune(rs, sp.col+sp.n)
		col, ok := semColor[sp.typ]
		if !ok {
			continue
		}
		for i := a; i < b && i < len(rs); i++ {
			colors[i] = col
		}
	}
	i := 0
	for i < len(rs) {
		j := i
		for j < len(rs) && colors[j] == colors[i] {
			j++
		}
		x += c.Text(x, y, string(rs[i:j]), 14, true, colors[i], clip)
		i = j
	}
}
func (g *GuiStudio) wrapLines(text string, width, size int, mono bool) []string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		rs := []rune(ln)
		if len(rs) == 0 {
			out = append(out, "")
		}
		for len(rs) > 0 {
			n := len(rs)
			for n > 1 && Measure(string(rs[:n]), size, mono) > width {
				n--
			}
			out = append(out, string(rs[:n]))
			rs = rs[n:]
		}
	}
	return out
}
func (g *GuiStudio) drawMarkdownBox(x, y, w int, md string, clip image.Rectangle) {
	c := g.UI.C
	inCode := false
	type ml struct {
		t    string
		code bool
	}
	var ls []ml
	for _, l := range strings.Split(md, "\n") {
		if strings.HasPrefix(l, "```") {
			inCode = !inCode
			continue
		}
		l = strings.ReplaceAll(strings.ReplaceAll(l, "**", ""), "`", "")
		for _, wl := range g.wrapLines(l, w-20, 13, inCode) {
			ls = append(ls, ml{wl, inCode})
		}
	}
	h := len(ls)*19 + 14
	if x+w > clip.Max.X {
		x = clip.Max.X - w - 4
	}
	box := image.Rect(x, y, x+w, y+h)
	c.Fill(box, tWidget)
	c.Border(box, rgb(0x454545))
	yy := y + 7
	for _, l := range ls {
		col := tInk
		if l.code {
			col = rgb(0x9cdcfe)
		}
		c.Text(x+10, yy, l.t, 13, l.code, col, box)
		yy += 19
	}
}

func (g *GuiStudio) drawPreview(L lay) {
	c := g.UI.C
	c.Fill(L.pvTabs, tTabIn)
	name := "预览 " + filepath.Base(g.S.Path)
	w := MeasureIconText(name, 13) + 46
	tr := image.Rect(L.pvTabs.Min.X, L.pvTabs.Min.Y, L.pvTabs.Min.X+w, L.pvTabs.Max.Y)
	c.Fill(tr, tEditor)
	c.Fill(image.Rect(tr.Min.X, tr.Min.Y, tr.Max.X, tr.Min.Y+2), tAccentB)
	c.IconText(tr.Min.X+12, tr.Min.Y+10, "open-preview", name, 13, false, tInkHi, tr)
	c.Icon(tr.Max.X-26, tr.Min.Y+10, "close", 16, tInk, tr)
	g.UI.Hits = append(g.UI.Hits, Hit{image.Rect(tr.Max.X-30, tr.Min.Y, tr.Max.X, tr.Max.Y), func() { g.PrevOpen = false }})
	g.iconBtn(L.pvTabs.Max.X-30, L.pvTabs.Min.Y+10, "refresh", tInk, L.pvTabs, func() { g.sync(); g.S.RebuildPreview(); g.AV.App = g.S.Preview })
	g.AV.App = g.S.Preview
	if g.AV.App != nil {
		g.AV.Render(g.UI, L.pv)
	} else {
		c.Fill(L.pv, tEditor)
		c.Text(L.pv.Min.X+20, L.pv.Min.Y+20, "修复错误后显示预览", 13, false, tMuted, L.pv)
	}
}

func (g *GuiStudio) drawPanel(r image.Rectangle) {
	c := g.UI.C
	s := g.S
	c.Fill(r, tSide)
	c.Fill(image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1), tBorder)
	e, w := g.errCount()
	tabs := [][2]string{{"problems", fmt.Sprintf("问题 %d", len(g.Diags))}, {"output", "输出"}, {"terminal", "终端"}, {"contracts", "契约"}, {"explain", "解释"}}
	x := r.Min.X + 16
	for _, t := range tabs {
		tw := Measure(t[1], 11, false)
		col := tMuted
		if g.PanelTab == t[0] {
			col = tInkHi
			c.Fill(image.Rect(x, r.Min.Y+30, x+tw, r.Min.Y+31), tAccentB)
		}
		c.Text(x, r.Min.Y+10, t[1], 11, false, col, r)
		id := t[0]
		g.UI.Hits = append(g.UI.Hits, Hit{image.Rect(x-6, r.Min.Y, x+tw+6, r.Min.Y+32), func() {
			g.PanelTab, g.PanelTop = id, 0
			if id == "terminal" {
				g.Focus = "terminal"
			}
		}})
		x += tw + 26
	}
	g.iconBtn(r.Max.X-60, r.Min.Y+8, "chevron-down", tInk, r, func() { g.PanelOpen = false })
	g.iconBtn(r.Max.X-32, r.Min.Y+8, "close", tInk, r, func() { g.PanelOpen = false })
	_ = e
	_ = w
	body := image.Rect(r.Min.X, r.Min.Y+36, r.Max.X, r.Max.Y)
	y := body.Min.Y + 4 - g.PanelTop
	lx := body.Min.X + 20
	text := func(t string, col colorT, mono bool) {
		for _, l := range g.wrapLines(t, body.Dx()-40, 13, mono) {
			c.Text(lx, y, l, 13, mono, col, body)
			y += 19
		}
	}
	switch g.PanelTab {
	case "problems":
		if len(g.Diags) == 0 {
			c.IconText(lx, y, "pass", "未在工作区检测到问题（类型、名称、效果、契约、视图引用均已检查）", 13, false, tInk, body)
		}
		if len(g.Diags) > 0 {
			c.Icon(lx-4, y+2, "chevron-down", 16, tInk, body)
			c.IconText(lx+14, y, "file-code", fmt.Sprintf("%s  %d", filepath.Base(s.Path), len(g.Diags)), 13, false, tInk, body)
			y += 24
		}
		for _, d := range g.Diags {
			icon, col := "error", tErr
			if d.Severity == 2 {
				icon, col = "warning", tWarn
			} else if d.Severity >= 3 {
				icon, col = "info", tInfo
			}
			rr := image.Rect(body.Min.X, y-2, body.Max.X, y+20)
			c.Icon(lx+16, y+1, icon, 16, col, body)
			tx := lx + 40 + c.Text(lx+40, y, d.Message, 13, false, tInk, body)
			c.Text(tx+10, y+1, fmt.Sprintf("aether(%s)  [行 %d, 列 %d]", d.Code, d.Range.Start.Line+1, d.Range.Start.Character+1), 12, false, tDim, body)
			p := d.Range.Start
			g.UI.Hits = append(g.UI.Hits, Hit{rr.Intersect(body), func() { g.gotoPos(p) }})
			y += 22
		}
	case "output":
		c.Text(body.Max.X-140, r.Min.Y+10, "Aether 语言服务器", 11, false, tMuted, r)
		if len(g.outLog) == 0 {
			text("[信息] aether-lsp "+Version+" 已在进程内启动；运行程序（Ctrl+R）、测试（Ctrl+T）和流程的输出显示在这里。", tMuted, false)
		}
		for _, l := range g.outLog[max(0, len(g.outLog)-200):] {
			col := tInk
			if strings.HasPrefix(l, "$") {
				col = tAccentB
			} else if strings.Contains(l, "错误") || strings.Contains(l, "失败") {
				col = tErr
			}
			text(l, col, true)
		}
	case "terminal":
		c.Text(body.Max.X-200, r.Min.Y+10, "aether（内置终端）", 11, false, tMuted, r)
		for _, l := range g.TermLog[max(0, len(g.TermLog)-300):] {
			col := tInk
			if strings.HasPrefix(l, "aether>") {
				col = tOK
			} else if strings.HasPrefix(l, "FAIL") || strings.HasPrefix(l, "错误") {
				col = tErr
			}
			text(l, col, true)
		}
		tw := c.Text(lx, y, "aether> ", 13, true, tOK, body)
		tw += c.Text(lx+tw, y, g.Term, 13, true, tInk, body)
		if g.Focus == "terminal" {
			c.Fill(image.Rect(lx+tw+1, y, lx+tw+9, y+17), rgb(0xaeafad))
		}
		g.UI.Hits = append(g.UI.Hits, Hit{body, func() { g.Focus = "terminal" }})
		if total := y + 30 - body.Min.Y + g.PanelTop; total > body.Dy() {
			g.PanelTop = total - body.Dy()
		}
	case "contracts":
		r2, _ := g.lsp("aether/contracts", nil).(map[string]any)
		items, _ := r2["items"].([]any)
		if len(items) == 0 {
			text("没有契约。fn/action 后写 requires / ensures，entity 中写 invariant，action 后写 allow 角色。", tMuted, false)
		}
		for _, it := range items {
			m := it.(map[string]any)
			icon := map[string]string{"invariant": "shield", "requires": "arrow-right", "ensures": "arrow-left", "allow": "lock"}[fmt.Sprint(m["kind"])]
			ln := int(m["line"].(float64))
			rr := image.Rect(body.Min.X, y-2, body.Max.X, y+20)
			c.Icon(lx, y+1, icon, 16, tAccentB, body)
			tx := lx + 24 + c.Text(lx+24, y, fmt.Sprintf("%v  %v", m["kind"], m["owner"]), 13, false, tInk, body)
			c.Text(tx+12, y, fmt.Sprint(m["text"]), 13, true, rgb(0x9cdcfe), body)
			g.UI.Hits = append(g.UI.Hits, Hit{rr.Intersect(body), func() { g.Ed.SetLineCol(ln-1, 0); g.Focus = "editor" }})
			y += 22
		}
	case "explain":
		r2, _ := g.lsp("aether/explain", nil).(map[string]any)
		for _, l := range strings.Split(fmt.Sprint(r2["text"]), "\n") {
			col := tInk
			if strings.HasPrefix(l, "#") {
				col, l = tAccentB, strings.TrimLeft(l, "# ")
			}
			text(strings.ReplaceAll(strings.ReplaceAll(l, "**", ""), "`", ""), col, false)
		}
	}
}

func (g *GuiStudio) drawAgent(r image.Rectangle) {
	u := g.UI
	c := u.C
	s := g.S
	c.Fill(r, tSide)
	mode := "mock"
	if envLLM() {
		mode = "LLM"
	}
	c.IconText(r.Min.X+16, r.Min.Y+10, "robot", "智能体  ·  "+mode, 11, false, tMuted, r)
	g.iconBtn(r.Max.X-32, r.Min.Y+9, "close", tInk, r, func() { g.AgentOpen = false })
	inputH := 92
	body := image.Rect(r.Min.X, r.Min.Y+36, r.Max.X, r.Max.Y-inputH)
	y := body.Min.Y + 4 - g.ChatTop
	lx := body.Min.X + 16
	wrap := func(text string, col colorT, size int, mono bool) {
		for _, l := range g.wrapLines(text, body.Dx()-36, size, mono) {
			c.Text(lx, y, l, size, mono, col, body)
			y += size + 6
		}
	}
	wrap("用自然语言描述修改；智能体返回结构化补丁（不是盲改文本），应用前由语言服务器做类型检查，由你决定接受或拒绝。补丁同样出现在编辑器的代码操作（Ctrl+.）中。", tMuted, 12, false)
	y += 4
	cx := lx
	name := "Todo"
	for _, sy := range g.Symbols {
		if sy["kind"] == float64(23) {
			name = fmt.Sprint(sy["name"])
			break
		}
	}
	for _, chip := range []string{"给 " + name + " 添加字段 due: Text", "添加统计", "修复错误", "把 " + name + " 改名为 Item"} {
		w := Measure(chip, 12, false) + 18
		if cx+w > body.Max.X-12 {
			cx = lx
			y += 28
		}
		rr := image.Rect(cx, y, cx+w, y+22)
		c.RoundRect(rr.Intersect(body), 4, tInput)
		c.Text(cx+9, y+4, chip, 12, false, tInk, rr.Intersect(body))
		ch := chip
		u.Hits = append(u.Hits, Hit{rr.Intersect(body), func() { g.ask(ch) }})
		cx += w + 6
	}
	y += 36
	for _, m := range s.Chat {
		if m.Me {
			c.IconText(lx, y, "account", "你", 12, false, tMuted, body)
			y += 20
			wrap(m.Text, tInkHi, 13, false)
			y += 8
			continue
		}
		p := m.Prop
		c.IconText(lx, y, "robot", "Aether 智能体 · "+p.Mode, 12, false, tAccentB, body)
		y += 20
		wrap(p.Summary, tInk, 13, false)
		if len(p.Ops) > 0 {
			box := image.Rect(lx-4, y, body.Max.X-12, y)
			lines := strings.Split(strings.TrimRight(p.Diff, "\n"), "\n")
			box.Max.Y = y + len(lines)*18 + 8
			c.Fill(box.Intersect(body), tEditor)
			c.Border(box.Intersect(body), tBorder)
			y += 4
			for _, dl := range lines {
				col := rgb(0xbbbbbb)
				bg := colorT{}
				switch {
				case strings.HasPrefix(dl, "+++") || strings.HasPrefix(dl, "---"):
					col = tDim
				case strings.HasPrefix(dl, "+"):
					col, bg = rgb(0x9ece9e), rgb(0x1e3a24)
				case strings.HasPrefix(dl, "-"):
					col, bg = rgb(0xf19999), rgb(0x4b1818)
				case strings.HasPrefix(dl, "@@"):
					col = tInfo
				}
				if bg != (colorT{}) {
					c.Fill(image.Rect(box.Min.X+1, y-1, box.Max.X-1, y+17).Intersect(body), bg)
				}
				c.Text(lx, y, dl, 12, true, col, box.Intersect(body))
				y += 18
			}
			y += 8
			if p.OK {
				c.IconText(lx, y, "pass", "补丁通过类型检查", 12, false, tOK, body)
			} else {
				c.IconText(lx, y, "warning", fmt.Sprintf("补丁后有 %d 个问题", len(p.Diags)), 12, false, tWarn, body)
			}
			y += 22
			switch m.Decision {
			case "":
				mm := m
				bx := lx
				bx += u.Button(bx, y, "$check 接受", tAccent, tInkHi, body, func() {
					s.Accept(mm)
					g.Ed.Set(s.Src)
					g.lspChange()
					g.AV.App = s.Preview
					g.Activity, g.SideOpen = "history", true
				}) + 8
				u.Button(bx, y, "$close 拒绝", tInput, tInk, body, func() { s.Reject(mm) })
				y += 34
			case "accepted":
				c.IconText(lx, y, "check-all", "已接受并应用（已记入历史）", 12, false, tOK, body)
				y += 22
			default:
				c.IconText(lx, y, "circle-slash", "已拒绝", 12, false, tMuted, body)
				y += 22
			}
		}
		y += 12
	}
	if total := y + g.ChatTop - body.Min.Y; g.ChatTop > max(0, total-body.Dy()+10) {
		g.ChatTop = max(0, total-body.Dy()+10)
	}
	ib := image.Rect(r.Min.X, r.Max.Y-inputH, r.Max.X, r.Max.Y)
	c.Fill(ib, tSide)
	box := image.Rect(ib.Min.X+12, ib.Min.Y+8, ib.Max.X-12, ib.Max.Y-12)
	bcol := rgb(0x3c3c3c)
	if g.Focus == "prompt" {
		bcol = tAccentB
	}
	c.Fill(box, tInput)
	c.Border(box, bcol)
	if g.Prompt == "" && g.Focus != "prompt" {
		c.Text(box.Min.X+10, box.Min.Y+10, "描述你想要的修改…  例如：给 "+name+" 添加字段 due: Text", 13, false, tDim, box)
	} else {
		lines := g.wrapLines(g.Prompt, box.Dx()-50, 13, false)
		yy := box.Min.Y + 10
		tw := 0
		for _, l := range lines {
			tw = c.Text(box.Min.X+10, yy, l, 13, false, tInk, box)
			yy += 19
		}
		if g.Focus == "prompt" {
			c.Fill(image.Rect(box.Min.X+11+tw, yy-19, box.Min.X+13+tw, yy-2), tInk)
		}
	}
	u.Hits = append(u.Hits, Hit{box, func() { g.Focus = "prompt" }})
	c.IconText(box.Min.X+10, box.Max.Y-22, "mention", "当前文件 "+filepath.Base(s.Path), 11, false, tMuted, box)
	g.iconBtn(box.Max.X-28, box.Max.Y-24, "send", tAccentB, box, func() { g.ask(g.Prompt) })
}

func (g *GuiStudio) drawStatus(L lay) {
	c := g.UI.C
	r := L.status
	c.Fill(r, tAccent)
	e, w := g.errCount()
	x := 10
	x += c.IconText(x, r.Min.Y+3, "source-control", "#"+g.S.Hash, 12, false, tInkHi, r) + 16
	x += c.IconText(x, r.Min.Y+3, "error", fmt.Sprint(e), 12, false, tInkHi, r) + 8
	x += c.IconText(x, r.Min.Y+3, "warning", fmt.Sprint(w), 12, false, tInkHi, r) + 16
	g.UI.Hits = append(g.UI.Hits, Hit{image.Rect(10, r.Min.Y, x, r.Max.Y), func() { g.exec("problems") }})
	if g.Notice != "" {
		c.Text(x, r.Min.Y+3, g.Notice, 12, false, tInkHi, image.Rect(x, r.Min.Y, L.W/2+100, r.Max.Y))
	}
	l, col := g.Ed.LineCol()
	mode := "mock"
	if envLLM() {
		mode = "LLM"
	}
	items := []string{fmt.Sprintf("行 %d，列 %d", l+1, col+1), "空格: 2", "UTF-8", "LF", "Aether", "$check-all LSP", "$robot " + mode, "$bell"}
	rx := L.W - 10
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		icon := ""
		if strings.HasPrefix(it, "$") {
			icon, it, _ = strings.Cut(it[1:], " ")
		}
		wd := Measure(it, 12, false)
		if icon != "" {
			wd += 20
		}
		rx -= wd
		if icon != "" {
			c.IconText(rx, r.Min.Y+3, icon, it, 12, false, tInkHi, r)
		} else {
			c.Text(rx, r.Min.Y+3, it, 12, false, tInkHi, r)
		}
		rx -= 16
	}
}

func (g *GuiStudio) drawPopupList(box image.Rectangle, p *popup, items []popItem, y int) {
	c := g.UI.C
	if p.Sel >= len(items) {
		p.Sel = max(0, len(items)-1)
	}
	start := max(0, p.Sel-11)
	for i := start; i < len(items) && y < box.Max.Y-22; i++ {
		it := items[i]
		rr := image.Rect(box.Min.X+4, y, box.Max.X-4, y+24)
		if i == p.Sel {
			c.Fill(rr, rgb(0x04395e))
		}
		x := rr.Min.X + 8
		if it.Icon != "" {
			c.Icon(x, y+4, it.Icon, 16, rgb(0x75beff), rr)
			x += 24
		}
		tw := c.Text(x, y+4, it.Label, 13, false, tInk, rr)
		if it.Detail != "" {
			dw := Measure(it.Detail, 12, false)
			dx := max(x+tw+16, rr.Max.X-dw-10)
			c.Text(dx, y+5, it.Detail, 12, false, tDim, rr)
		}
		run := it.Run
		pp := p
		g.UI.Hits = append(g.UI.Hits, Hit{rr, func() {
			if g.Pal == pp {
				g.Pal = nil
			}
			if g.Comp == pp {
				g.Comp = nil
			}
			run()
		}})
		y += 24
	}
}
func (g *GuiStudio) drawPalette(L lay) {
	c := g.UI.C
	p := g.Pal
	w := 620
	x := L.W/2 - w/2
	items := p.Items
	if p.Filter != nil {
		items = p.Filter(p.Input)
	}
	h := 44 + min(len(items), 12)*24 + 8
	if p.Title != "" {
		h += 22
	}
	box := image.Rect(x, 36, x+w, 36+h)
	c.Fill(image.Rect(box.Min.X+3, box.Min.Y+3, box.Max.X+3, box.Max.Y+3), rgb(0x0a0a0a))
	c.Fill(box, tWidget)
	c.Border(box, rgb(0x454545))
	y := box.Min.Y + 8
	if p.Title != "" {
		c.Text(box.Min.X+12, y, p.Title, 12, false, tMuted, box)
		y += 22
	}
	in := image.Rect(box.Min.X+8, y, box.Max.X-8, y+28)
	c.Fill(in, tInput)
	c.Border(in, tAccentB)
	q := p.Input
	ph := ""
	if q == "" {
		ph = "> 输入命令名称；@ 转到符号"
	}
	tw := c.Text(in.Min.X+8, in.Min.Y+6, q, 13, false, tInk, in)
	if ph != "" {
		c.Text(in.Min.X+8, in.Min.Y+6, ph, 13, false, tDim, in)
	}
	c.Fill(image.Rect(in.Min.X+9+tw, in.Min.Y+5, in.Min.X+10+tw, in.Max.Y-5), tInk)
	g.drawPopupList(box, p, items, y+36)
}
func (g *GuiStudio) drawCompletion(L lay) {
	c := g.UI.C
	p := g.Comp
	l, cc := g.Ed.LineCol()
	x := L.ed.Min.X + 64 + Measure(string([]rune(g.Ed.Lines()[l])[:cc]), 14, true)
	y := L.ed.Min.Y + 4 + (l-g.Ed.Top+1)*20
	w := 520
	if x+w > L.W-10 {
		x = L.W - w - 10
	}
	h := min(len(p.Items), 10)*24 + 34
	if y+h > L.H-30 {
		y = y - h - 24
	}
	box := image.Rect(x, y, x+w, y+h)
	c.Fill(box, tWidget)
	c.Border(box, rgb(0x454545))
	c.Text(box.Min.X+10, box.Min.Y+6, p.Title+"（Enter 应用 · Esc 关闭）", 11, false, tMuted, box)
	g.drawPopupList(box, p, p.Items, box.Min.Y+26)
}

func (g *GuiStudio) clipSet(t string) {
	if cb, ok := g.Win.(Clipboard); ok {
		cb.SetClipboard(t)
		return
	}
	memClip = t
}
func (g *GuiStudio) clipGet() string {
	if cb, ok := g.Win.(Clipboard); ok {
		return cb.GetClipboard()
	}
	return memClip
}

// mouseMove records the pointer; returns true if the screen must be redrawn.
func (g *GuiStudio) mouseMove(x, y int) bool {
	g.mouseX, g.mouseY, g.moveAt, g.hoverDone = x, y, time.Now(), false
	if g.hoverMouse && g.Hover != "" {
		L := g.layout()
		line := g.Ed.Top + (y-L.ed.Min.Y-4)/20
		if line != g.hoverLine {
			g.Hover, g.hoverMouse = "", false
			return true
		}
	}
	return false
}

// hoverTick shows LSP hover for the word under a resting mouse pointer (VS Code-like, 500 ms).
func (g *GuiStudio) hoverTick() bool {
	if g.hoverDone || g.moveAt.IsZero() || time.Since(g.moveAt) < 500*time.Millisecond || g.Pal != nil || g.Comp != nil {
		return false
	}
	g.hoverDone = true
	L := g.layout()
	if !(image.Point{g.mouseX, g.mouseY}).In(L.ed) || g.mouseX < L.ed.Min.X+64 {
		return false
	}
	lines := g.Ed.Lines()
	line := g.Ed.Top + (g.mouseY-L.ed.Min.Y-4)/20
	if line < 0 || line >= len(lines) {
		return false
	}
	rs := []rune(lines[line])
	col := 0
	for col < len(rs) && L.ed.Min.X+64+Measure(string(rs[:col+1]), 14, true) <= g.mouseX {
		col++
	}
	if col >= len(rs) || rs[col] == ' ' {
		return false
	}
	g.sync()
	r, ok := g.lsp("textDocument/hover", map[string]any{"position": LPos{line, utf16Len(string(rs[:col]))}}).(map[string]any)
	if !ok {
		return false
	}
	start := col
	for start > 0 && (unicode.IsLetter(rs[start-1]) || unicode.IsDigit(rs[start-1]) || rs[start-1] == '_') {
		start--
	}
	g.Hover = fmt.Sprint(r["contents"].(map[string]any)["value"])
	g.hoverMouse, g.hoverLine, g.hoverCol = true, line, start
	return true
}
