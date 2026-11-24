package main

// `aether lsp`: a Language Server Protocol (JSON-RPC 2.0 over stdio) server for Aether.
// Used by the VS Code / Aether IDE extension and, in-process, by the native Studio.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type lspDoc struct {
	URI, Text string
	dir       string
	Version   int
	an        *Analysis
	preview   *App
	props     map[int]*Proposal
	nextProp  int
}

type LSPServer struct {
	mu       sync.Mutex
	wmu      sync.Mutex
	out      io.Writer
	docs     map[string]*lspDoc
	shutdown bool
	prevSrv  string
	OnNotify func(method string, params any) // in-process observers (Studio)
	HistFor  func(uri string) *History       // in-process: share the Studio's history object
}

func NewLSP(out io.Writer) *LSPServer { return &LSPServer{out: out, docs: map[string]*lspDoc{}} }

type rpcMsg struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   any              `json:"error,omitempty"`
}

func ReadLSPMessage(r *bufio.Reader) ([]byte, error) {
	n := -1
	headerBytes := 0
	for {
		lineBytes, err := r.ReadSlice('\n')
		if err != nil {
			return nil, err
		}
		headerBytes += len(lineBytes)
		if headerBytes > 8192 {
			return nil, fmt.Errorf("LSP headers exceed limit")
		}
		line := strings.TrimRight(string(lineBytes), "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			if n != -1 {
				return nil, fmt.Errorf("duplicate Content-Length")
			}
			value, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || value < 0 || value > 16<<20 {
				return nil, fmt.Errorf("invalid Content-Length (limit 16 MiB)")
			}
			n = value
		}
	}
	if n < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}
func WriteLSPMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return err
}

func (s *LSPServer) send(v any) {
	if s.out == nil {
		return
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	WriteLSPMessage(s.out, v)
}
func (s *LSPServer) notify(method string, params any) {
	if s.OnNotify != nil {
		s.OnNotify(method, params)
	}
	s.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// Serve runs the stdio loop until exit.
func (s *LSPServer) Serve(in io.Reader) error {
	r := bufio.NewReaderSize(in, 1<<16)
	for {
		b, err := ReadLSPMessage(r)
		if err != nil {
			return err
		}
		var m rpcMsg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.Method == "exit" {
			return nil
		}
		res, rerr := s.Handle(m.Method, m.Params)
		if m.ID != nil {
			resp := map[string]any{"jsonrpc": "2.0", "id": m.ID}
			if rerr != nil {
				resp["error"] = map[string]any{"code": -32603, "message": rerr.Error()}
				if rerr == errMethod {
					resp["error"] = map[string]any{"code": -32601, "message": "method not found: " + m.Method}
				}
			} else {
				resp["result"] = res
			}
			s.send(resp)
		}
	}
}

var errMethod = fmt.Errorf("method not found")

var semTypes = []string{"namespace", "type", "class", "enum", "enumMember", "parameter", "variable", "property", "function", "method", "event", "keyword", "string", "number", "operator", "comment"}
var semMods = []string{"declaration", "defaultLibrary", "readonly"}

type tdParams struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Text    string `json:"text"`
		Version int    `json:"version"`
	} `json:"textDocument"`
	Position       LPos   `json:"position"`
	Range          LRange `json:"range"`
	NewName        string `json:"newName"`
	ContentChanges []struct {
		Text  string  `json:"text"`
		Range *LRange `json:"range"`
	} `json:"contentChanges"`
	Context struct {
		Diagnostics        []map[string]any `json:"diagnostics"`
		IncludeDeclaration bool             `json:"includeDeclaration"`
	} `json:"context"`
	// custom
	Prompt  string          `json:"prompt"`
	Symbol  string          `json:"symbol"`
	Name    string          `json:"name"`
	Method  string          `json:"method"`
	Path    string          `json:"path"`
	Body    json.RawMessage `json:"body"`
	Dev     bool            `json:"dev"`
	Token   string          `json:"token"`
	Role    string          `json:"role"`
	ID      int             `json:"id"`
	Command string          `json:"command"`
	Args    []any           `json:"arguments"`
}

func (s *LSPServer) doc(uri string) *lspDoc {
	d := s.docs[uri]
	if d == nil {
		p := uriPath(uri)
		b, _ := os.ReadFile(p)
		d = &lspDoc{URI: uri, dir: filepath.Dir(uriPath(uri)), Text: string(b), props: map[int]*Proposal{}}
		s.docs[uri] = d
	}
	if d.an == nil || d.an.Text != d.Text {
		d.an = Analyze(d.Text)
	}
	return d
}
func uriPath(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" {
		return u.Path
	}
	return strings.TrimPrefix(uri, "file://")
}

// Handle dispatches one request/notification; usable in-process.
func (s *LSPServer) Handle(method string, raw json.RawMessage) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p tdParams
	json.Unmarshal(raw, &p)
	uri := p.TextDocument.URI
	switch method {
	case "initialize":
		return map[string]any{
			"serverInfo": map[string]any{"name": "aether-lsp", "version": Version},
			"capabilities": map[string]any{
				"positionEncoding":           "utf-16",
				"textDocumentSync":           map[string]any{"openClose": true, "change": 2, "save": map[string]any{"includeText": false}},
				"hoverProvider":              true,
				"completionProvider":         map[string]any{"triggerCharacters": []string{".", " "}},
				"definitionProvider":         true,
				"referencesProvider":         true,
				"renameProvider":             map[string]any{"prepareProvider": true},
				"documentSymbolProvider":     true,
				"documentFormattingProvider": true,
				"codeActionProvider":         map[string]any{"codeActionKinds": []string{"quickfix", "refactor.rewrite"}},
				"executeCommandProvider":     map[string]any{"commands": []string{"aether.acceptProposal"}},
				"semanticTokensProvider": map[string]any{"legend": map[string]any{"tokenTypes": semTypes, "tokenModifiers": semMods},
					"full": true, "range": false},
				"experimental": map[string]any{"aether": []string{"aether/explain", "aether/contracts", "aether/tests", "aether/run", "aether/runFlow", "aether/flows", "aether/preview", "aether/previewRequest", "aether/history", "aether/undo", "aether/redo", "aether/propose", "aether/ast", "aether/agentSchema"}},
			},
		}, nil
	case "initialized", "$/cancelRequest", "$/setTrace", "workspace/didChangeConfiguration", "workspace/didChangeWatchedFiles":
		return nil, nil
	case "shutdown":
		s.shutdown = true
		return nil, nil
	case "textDocument/didOpen":
		s.docs[uri] = &lspDoc{URI: uri, dir: filepath.Dir(uriPath(uri)), Text: p.TextDocument.Text, Version: p.TextDocument.Version, props: map[int]*Proposal{}}
		s.hist(uri) // baseline entry for the opened text
		s.publish(uri)
		return nil, nil
	case "textDocument/didChange":
		d := s.docs[uri]
		if d == nil {
			return nil, fmt.Errorf("document is not open")
		}
		if p.TextDocument.Version <= d.Version {
			return nil, nil
		}
		text := d.Text
		for _, c := range p.ContentChanges {
			if c.Range == nil {
				text = c.Text
				continue
			}
			start, end := c.Range.Start, c.Range.End
			if start.Line < 0 || start.Character < 0 || end.Line < 0 || end.Character < 0 {
				return nil, fmt.Errorf("invalid edit range")
			}
			a, b := PosToOff(text, start), PosToOff(text, end)
			if a > b || OffToPos(text, a) != start || OffToPos(text, b) != end {
				return nil, fmt.Errorf("invalid UTF-16 edit range")
			}
			text = text[:a] + c.Text + text[b:]
		}
		d.Text = text
		d.Version = p.TextDocument.Version
		d.preview = nil
		s.publish(uri)
		return nil, nil
	case "textDocument/didSave":
		d := s.doc(uri)
		if h := s.hist(uri); h != nil {
			if hd := h.Head(); hd == nil || hd.Hash != ASTHash(d.Text) {
				h.Commit(d.Text, "human", "保存（编辑器）", nil, "save")
			}
		}
		return nil, nil
	case "textDocument/didClose":
		delete(s.docs, uri)
		s.notify("textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": []any{}})
		return nil, nil
	case "textDocument/hover":
		return s.hover(s.doc(uri), p.Position), nil
	case "textDocument/completion":
		return s.complete(s.doc(uri), p.Position), nil
	case "textDocument/definition":
		d := s.doc(uri)
		sym, _, _ := d.an.SymAt(PosToOff(d.Text, p.Position))
		if sym == nil || sym.Tok.End == 0 {
			return nil, nil
		}
		return map[string]any{"uri": uri, "range": Rng(d.Text, sym.Tok.Pos, sym.Tok.End)}, nil
	case "textDocument/references":
		d := s.doc(uri)
		sym, _, _ := d.an.SymAt(PosToOff(d.Text, p.Position))
		out := []any{}
		if sym != nil {
			for _, r := range d.an.References(sym, p.Context.IncludeDeclaration) {
				out = append(out, map[string]any{"uri": uri, "range": Rng(d.Text, r[0], r[1])})
			}
		}
		return out, nil
	case "textDocument/prepareRename":
		d := s.doc(uri)
		sym, o, ok := d.an.SymAt(PosToOff(d.Text, p.Position))
		if !ok || sym == nil {
			return nil, fmt.Errorf("这里没有可重命名的符号")
		}
		return map[string]any{"range": Rng(d.Text, o.Pos, o.End), "placeholder": sym.Name}, nil
	case "textDocument/rename":
		d := s.doc(uri)
		if !regexp.MustCompile(`^[A-Za-z_\x{4e00}-\x{9fff}][A-Za-z0-9_\x{4e00}-\x{9fff}]*$`).MatchString(p.NewName) || keywords[p.NewName] {
			return nil, fmt.Errorf("'%s' 不是合法的标识符", p.NewName)
		}
		sym, _, _ := d.an.SymAt(PosToOff(d.Text, p.Position))
		if sym == nil {
			return nil, fmt.Errorf("这里没有可重命名的符号")
		}
		edits := []any{}
		for _, r := range d.an.References(sym, true) {
			edits = append(edits, map[string]any{"range": Rng(d.Text, r[0], r[1]), "newText": p.NewName})
		}
		return map[string]any{"changes": map[string]any{uri: edits}}, nil
	case "textDocument/documentSymbol":
		return s.symbols(s.doc(uri)), nil
	case "textDocument/formatting":
		d := s.doc(uri)
		f, err := Fmt(d.Text)
		if err != nil || f == d.Text {
			return []any{}, nil
		}
		return []any{map[string]any{"range": Rng(d.Text, 0, len(d.Text)), "newText": f}}, nil
	case "textDocument/semanticTokens/full":
		return map[string]any{"data": s.semTokens(s.doc(uri))}, nil
	case "textDocument/codeAction":
		return s.codeActions(s.doc(uri), p.Range, p.Context.Diagnostics), nil
	case "workspace/executeCommand":
		if p.Command == "aether.acceptProposal" && len(p.Args) >= 2 {
			u, _ := p.Args[0].(string)
			id, _ := p.Args[1].(float64)
			return s.accept(u, int(id)), nil
		}
		return nil, nil
	// ---------- Aether custom requests ----------
	case "aether/explain":
		t, err := Explain(s.doc(uri).Text)
		return map[string]any{"text": t, "error": errStr(err), "hash": ASTHash(s.doc(uri).Text)}, nil
	case "aether/ast":
		ast, diags := CheckSourceIn(s.doc(uri).Text, filepath.Dir(uriPath(uri)))
		return map[string]any{"ast": astAny(ast), "diagnostics": diagList(diags)}, nil
	case "aether/contracts":
		return map[string]any{"items": contractsOf(s.doc(uri).Text)}, nil
	case "aether/tests":
		it := NewInterp(":memory:", false)
		if err := it.Load(s.doc(uri).Text, filepath.Dir(uriPath(uri))); err != nil {
			return map[string]any{"results": []any{}, "error": err.Error()}, nil
		}
		return map[string]any{"results": testsAny(it.RunTests())}, nil
	case "aether/run":
		it := NewInterp(":memory:", false)
		err := it.Load(s.doc(uri).Text, filepath.Dir(uriPath(uri)))
		e := ""
		if t, ok := err.(*AeThrow); ok {
			e = "uncaught: " + show(t.V)
		} else if err != nil {
			e = err.Error()
		}
		return map[string]any{"output": append([]string{}, it.Out...), "error": e}, nil
	case "aether/flows":
		out := []any{}
		for _, g := range s.doc(uri).an.Globals {
			if g.Kind == "flow" {
				out = append(out, g.Name)
			}
		}
		return map[string]any{"flows": out}, nil
	case "aether/runFlow":
		app, err := s.previewApp(s.doc(uri))
		if err != nil {
			return nil, err
		}
		var res any
		json.Unmarshal([]byte(toJSON(app.It.RunFlow(p.Name, nil))), &res)
		return res, nil
	case "aether/previewRequest":
		app, err := s.previewApp(s.doc(uri))
		if err != nil {
			return nil, err
		}
		role := strOr(p.Role, "user")
		if p.Dev {
			role = "\x01dev"
		} else if p.Token != "" {
			role += "\x00" + p.Token
		}
		code, ct, body := app.Handle(strOr(p.Method, "GET"), p.Path, p.Body, role)
		return map[string]any{"status": code, "contentType": ct, "body": string(body)}, nil
	case "aether/preview":
		return map[string]any{"url": s.previewURL(uri)}, nil
	case "aether/history":
		h := s.hist(uri)
		if h == nil {
			return map[string]any{"entries": []any{}}, nil
		}
		ents := []any{}
		for i, e := range h.Entries {
			ents = append(ents, map[string]any{"id": e.ID, "hash": e.Hash, "author": e.Author, "kind": e.Kind, "reason": e.Reason, "time": e.Time, "ops": len(e.Ops), "head": i == h.Pos, "undone": i > h.Pos})
		}
		return map[string]any{"entries": ents, "pos": h.Pos}, nil
	case "aether/undo", "aether/redo":
		h := s.hist(uri)
		if h == nil {
			return nil, fmt.Errorf("没有历史")
		}
		d := s.doc(uri)
		if hd := h.Head(); hd == nil || hd.Hash != ASTHash(d.Text) {
			h.Commit(d.Text, "human", "编辑", nil, "edit")
		}
		var src string
		var err error
		if method == "aether/undo" {
			src, err = h.Undo()
		} else {
			src, err = h.Redo()
		}
		if err != nil {
			return map[string]any{"ok": false, "status": "没有可" + map[bool]string{true: "撤销", false: "重做"}[method == "aether/undo"] + "的"}, nil
		}
		return map[string]any{"ok": true, "source": src, "hash": h.Head().Hash, "edit": fullEdit(uri, d.Text, src)}, nil
	case "aether/propose":
		d := s.doc(uri)
		pr := Propose(d.Text, p.Prompt)
		d.nextProp++
		d.props[d.nextProp] = &pr
		r := pr.JSON()
		r["id"] = d.nextProp
		r["edit"] = fullEdit(uri, d.Text, pr.Source)
		return r, nil
	case "aether/context":
		return AgentContext(s.doc(uri).Text, p.Symbol, filepath.Dir(uriPath(uri)))
	case "aether/compact":
		c, err := Compact(s.doc(uri).Text)
		if err != nil {
			return nil, err
		}
		sk, _ := Skeleton(s.doc(uri).Text)
		return map[string]any{"compact": c, "skeleton": sk}, nil
	case "aether/model":
		return ArchModel(s.doc(uri).Text, filepath.Dir(uriPath(uri))), nil
	case "aether/flowRuns":
		app, err := s.previewApp(s.doc(uri))
		if err != nil {
			return nil, err
		}
		var res any
		json.Unmarshal([]byte(toJSON(app.It.FlowRuns())), &res)
		return map[string]any{"runs": res}, nil
	case "aether/resumeFlow":
		app, err := s.previewApp(s.doc(uri))
		if err != nil {
			return nil, err
		}
		var res any
		if e := catch(func() { json.Unmarshal([]byte(toJSON(app.It.ResumeFlow(int64(p.ID)))), &res) }); e != nil {
			return nil, e
		}
		return res, nil
	case "aether/agentSchema":
		return map[string]any{"ops": OpsSchema, "language": LangBrief}, nil
	}
	if strings.HasPrefix(method, "$/") {
		return nil, nil
	}
	return nil, errMethod
}
func errStr(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
func fullEdit(uri, old, src string) map[string]any {
	return map[string]any{"changes": map[string]any{uri: []any{map[string]any{"range": Rng(old, 0, len(old)), "newText": src}}}}
}

var histCache = map[string]*History{}

func (s *LSPServer) hist(uri string) *History {
	if s.HistFor != nil {
		return s.HistFor(uri)
	}
	p := uriPath(uri)
	if p == "" || !strings.HasPrefix(uri, "file:") {
		return nil
	}
	if h, ok := histCache[p]; ok {
		return h
	}
	h := OpenHistory(p + ".history.json")
	if h.Head() == nil {
		if d := s.docs[uri]; d != nil {
			h.Commit(d.Text, "human", "打开文件（基线）", nil, "baseline")
		}
	}
	histCache[p] = h
	return h
}

func (s *LSPServer) accept(uri string, id int) any {
	d := s.doc(uri)
	pr := d.props[id]
	if pr == nil {
		return map[string]any{"ok": false}
	}
	if h := s.hist(uri); h != nil {
		h.Commit(pr.Source, "agent:"+pr.Mode, pr.Prompt+" → "+pr.Summary, pr.Ops, "agent-patch")
	}
	delete(d.props, id)
	return map[string]any{"ok": true}
}

func (s *LSPServer) previewApp(d *lspDoc) (*App, error) {
	if d.preview != nil && d.preview.Src == d.Text {
		return d.preview, nil
	}
	app, err := NewApp(d.Text, ":memory:", d.dir)
	if err != nil {
		if d.preview != nil {
			return d.preview, nil // keep last good preview while the user types
		}
		return nil, err
	}
	if old := d.preview; old != nil {
		for _, sys := range []Node{userDecl, secretDecl, flowRunDecl} {
			old.It.Store.Ensure(sys)
			app.It.Store.Ensure(sys)
			for _, r := range old.It.Store.All(sys["name"].(string)) {
				app.It.Store.Insert(sys["name"].(string), r)
			}
		}
		for _, e := range app.It.EntOrder {
			if _, ok := old.It.Entities[e]; ok {
				for _, r := range old.It.Store.All(e) {
					app.It.Store.Insert(e, r)
				}
			}
		}
	}
	d.preview = app
	return app, nil
}

// previewURL starts (once) a loopback HTTP server that serves live previews of open documents.
func (s *LSPServer) previewURL(uri string) string {
	if s.prevSrv == "" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return ""
		}
		s.prevSrv = "http://" + ln.Addr().String()
		go http.Serve(ln, httpHandler(func(method, path string, body []byte, role string) (int, string, []byte) {
			// /d/<n>/... selects the document
			parts := strings.SplitN(strings.TrimPrefix(path, "/d/"), "/", 2)
			s.mu.Lock()
			defer s.mu.Unlock()
			var d *lspDoc
			for u, dd := range s.docs {
				if docKey(u) == parts[0] {
					d = dd
				}
			}
			if d == nil {
				return errResp(404, "document not open")
			}
			app, err := s.previewApp(d)
			if err != nil {
				return errResp(400, err.Error())
			}
			rest := "/"
			if len(parts) > 1 {
				rest += parts[1]
			}
			return app.Handle(method, rest, body, role)
		}))
	}
	return s.prevSrv + "/d/" + docKey(uri) + "/"
}
func docKey(uri string) string { return ASTHash(uri)[:8] }

// ---------- diagnostics ----------
var quoted = regexp.MustCompile(`'([^']+)'`)

func (s *LSPServer) diagnostics(d *lspDoc) []any {
	_, ds := CheckSourceIn(d.Text, d.dir)
	out := []any{}
	lines := strings.Split(d.Text, "\n")
	for _, x := range ds {
		l, _ := x["line"].(int)
		li := max(0, l-1)
		if li >= len(lines) {
			li = len(lines) - 1
		}
		lineStart := 0
		for i := 0; i < li; i++ {
			lineStart += len(lines[i]) + 1
		}
		ln := lines[li]
		a, b := lineStart+len(ln)-len(strings.TrimLeft(ln, " \t")), lineStart+len(strings.TrimRight(ln, " \t\r"))
		if m := quoted.FindStringSubmatch(fmt.Sprint(x["message"])); m != nil {
			if k := identIndex(ln, m[1]); k >= 0 {
				a, b = lineStart+k, lineStart+k+len(m[1])
			}
		}
		if b <= a {
			b = a + 1
			if b > len(d.Text) {
				b = len(d.Text)
			}
		}
		sev := map[string]int{"error": 1, "warning": 2, "info": 3}[fmt.Sprint(x["severity"])]
		if sev == 0 {
			sev = 3
		}
		dg := map[string]any{"range": Rng(d.Text, a, b), "severity": sev, "code": x["code"], "source": "aether", "message": x["message"]}
		if c, ok := x["code"].(string); ok {
			if ed, ok := ErrDocs[c]; ok {
				dg["message"] = fmt.Sprint(x["message"]) + "\n" + ed.Title + "：" + ed.Fix
			}
		}
		if _, ok := x["hint"]; !ok && x["code"] == "E200" {
			if m := quoted.FindStringSubmatch(fmt.Sprint(x["message"])); m != nil {
				if sug := suggestName(d.an, m[1]); sug != "" {
					x["hint"] = "did you mean '" + sug + "'?"
				}
			}
		}
		if h, ok := x["hint"].(string); ok {
			dg["message"] = fmt.Sprint(x["message"]) + "（提示：" + h + "）"
			dg["data"] = map[string]any{"hint": h}
		}
		out = append(out, dg)
	}
	return out
}
func identIndex(line, name string) int {
	for i := 0; ; {
		k := strings.Index(line[i:], name)
		if k < 0 {
			return -1
		}
		k += i
		okL := k == 0 || !isIdPartByte(line, k-1)
		okR := k+len(name) >= len(line) || !isIdPartByte(line, k+len(name))
		if okL && okR {
			return k
		}
		i = k + 1
	}
}
func isIdPartByte(s string, i int) bool {
	c := s[i]
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}
func (s *LSPServer) publish(uri string) {
	d := s.doc(uri)
	s.notify("textDocument/publishDiagnostics", map[string]any{"uri": uri, "version": d.Version, "diagnostics": s.diagnostics(d)})
}

// ---------- hover ----------
var kwDocs = map[string]string{
	"entity": "**entity** 声明一个持久化实体：同时生成数据表（AeStore）、REST 接口和表单。",
	"view":   "**view** 声明一个界面页面：title/stat/form/list/table/chart/button 等组件。",
	"action": "**action** 声明一个可从界面和 API 调用的业务动作；可带 requires/ensures/allow。",
	"fn":     "**fn** 纯函数（或带效果声明 `! db` 等的函数）。",
	"flow":   "**flow** 可执行的多步骤流程；每个 step 记录结果与耗时。", "step": "**step** 流程中的一步。",
	"requires": "**requires** 前置条件：调用前必须成立，否则 R041。", "ensures": "**ensures** 后置条件：`result` 指返回值，不成立则 R042。",
	"invariant": "**invariant** 实体不变式：每次 insert/save 都会检查，不成立则 R040。", "allow": "**allow** 角色权限：只有列出的角色可以调用该 action（否则 403）。",
	"test": "**test** 内嵌测试块，`aether test` 运行。", "example": "**example** 可执行示例：`example f(1) == 2`，同时是文档和测试。",
	"spawn": "**spawn** 并发启动任务，返回 Future。", "await": "**await** 等待 Future 的结果。", "match": "**match** 模式匹配（代数数据类型）。",
	"type": "**type** 代数数据类型：`type Shape = Circle(r: Float) | Dot`。", "module": "**module** 模块名。",
}

func md(s string) map[string]any { return map[string]any{"kind": "markdown", "value": s} }

func (s *LSPServer) hover(d *lspDoc, p LPos) any {
	off := PosToOff(d.Text, p)
	i := d.an.TokAt(off)
	if i < 0 {
		return nil
	}
	t := d.an.Toks[i]
	rng := Rng(d.Text, t.Pos, t.End)
	if t.Kind == "kw" {
		if doc, ok := kwDocs[t.S]; ok {
			return map[string]any{"contents": md(doc), "range": rng}
		}
		return nil
	}
	sym, _, _ := d.an.SymAt(off)
	kindZh := map[string]string{"entity": "实体", "fn": "函数", "action": "动作", "view": "视图", "flow": "流程", "type": "类型", "variant": "构造器", "field": "字段", "param": "参数", "local": "局部变量", "loopvar": "循环变量", "global": "全局变量", "module": "模块"}
	if sym != nil {
		body := "```aether\n" + sym.Detail + "\n```"
		head := kindZh[sym.Kind] + " `" + sym.Name + "`"
		if sym.Parent != "" && sym.Kind == "field" {
			head += "（属于 " + sym.Parent + "）"
		}
		if sym.Kind == "entity" {
			head += "\n\n自动提供：`GET/POST /api/" + sym.Name + "`、表单、列表，数据存于 AeStore（WAL）。"
		}
		if sym.Kind == "action" {
			head += "\n\n可通过 `POST /api/action/" + sym.Name + "` 调用。"
		}
		return map[string]any{"contents": md(head + "\n\n" + body), "range": rng}
	}
	if builtinTypeNames[t.S] {
		return map[string]any{"contents": md("内置类型 `" + t.S + "`"), "range": rng}
	}
	if builtinNames[t.S] {
		e := EFFECTS[t.S]
		txt := "内置函数 `" + t.S + "`"
		if e != "" {
			txt += "\n\n效果：`! " + e + "`（调用者必须声明该效果）"
		} else {
			txt += "\n\n纯函数（无效果）"
		}
		return map[string]any{"contents": md(txt), "range": rng}
	}
	return nil
}

// ---------- completion ----------
var viewItemKinds = []string{"title", "text", "stat", "form", "list", "table", "chart", "button"}
var snippets = [][3]string{
	{"entity", "entity ${1:Name} {\n  ${2:title}: ${3:Text}\n}", "实体"},
	{"action", "action ${1:name}(${2:x}: ${3:Entity}) {\n  $0\n}", "动作"},
	{"fn", "fn ${1:name}(${2:x}: ${3:Int}) -> ${4:Int} {\n  $0\n}", "函数"},
	{"view", "view ${1:首页} {\n  title \"${2:标题}\"\n  $0\n}", "视图"},
	{"test", "test \"${1:描述}\" {\n  $0\n}", "测试"},
	{"flow", "flow ${1:名称} {\n  step \"${2:第一步}\" { $0 }\n}", "流程"},
}

func (s *LSPServer) complete(d *lspDoc, p LPos) any {
	off := PosToOff(d.Text, p)
	k := off
	for k > 0 && isIdPartByte(d.Text, k-1) {
		k--
	}
	items := []any{}
	seen := map[string]bool{}
	addI := func(label string, kind int, detail string) {
		if seen[label] {
			return
		}
		seen[label] = true
		items = append(items, map[string]any{"label": label, "kind": kind, "detail": detail})
	}
	if k > 0 && d.Text[k-1] == '.' {
		for _, f := range d.an.Fields {
			addI(f.Name, 5, f.Parent+" · "+f.Detail)
		}
		addI("id", 5, "记录 id")
		return map[string]any{"isIncomplete": false, "items": items}
	}
	inView := false
	for _, g := range d.an.Globals {
		if g.Kind == "view" && off > g.Range[0] && off < g.Range[1] {
			inView = true
		}
	}
	if inView {
		for _, v := range viewItemKinds {
			addI(v, 14, "视图组件")
		}
	}
	for _, sn := range snippets {
		items = append(items, map[string]any{"label": sn[0] + " …", "kind": 15, "insertText": sn[1], "insertTextFormat": 2, "detail": "片段：" + sn[2], "filterText": sn[0]})
	}
	ck := map[string]int{"entity": 7, "fn": 3, "action": 2, "view": 9, "flow": 23, "type": 13, "variant": 20, "global": 6, "module": 9}
	for _, l := range d.an.Locals {
		if off >= l.Scope[0] && off <= l.Scope[1] && l.Tok.Pos < off {
			addI(l.Name, 6, strings.TrimSpace(l.Detail))
		}
	}
	for _, g := range d.an.Globals {
		if c, ok := ck[g.Kind]; ok {
			addI(g.Name, c, firstLine(g.Detail))
		}
	}
	for t := range builtinTypeNames {
		addI(t, 22, "内置类型")
	}
	bn := []string{}
	for b := range builtinNames {
		bn = append(bn, b)
	}
	sort.Strings(bn)
	for _, b := range bn {
		det := "内置函数"
		if e := EFFECTS[b]; e != "" {
			det += " ! " + e
		}
		addI(b, 3, det)
	}
	kws := []string{}
	for w := range keywords {
		kws = append(kws, w)
	}
	sort.Strings(kws)
	for _, w := range kws {
		addI(w, 14, "关键字")
	}
	return map[string]any{"isIncomplete": false, "items": items}
}
func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }

// ---------- symbols ----------
func (s *LSPServer) symbols(d *lspDoc) []any {
	kinds := map[string]int{"module": 2, "entity": 23, "fn": 12, "action": 6, "view": 5, "flow": 24, "type": 10, "variant": 22, "field": 8, "test": 12, "global": 13, "step": 24}
	var conv func(sy *Sym) map[string]any
	conv = func(sy *Sym) map[string]any {
		r := sy.Range
		if r[1] <= r[0] {
			r = [2]int{sy.Tok.Pos, sy.Tok.End}
		}
		sel := [2]int{sy.Tok.Pos, sy.Tok.End}
		if sel[0] < r[0] || sel[1] > r[1] {
			sel = r
		}
		m := map[string]any{"name": sy.Name, "kind": kinds[sy.Kind], "detail": sy.Kind, "range": Rng(d.Text, r[0], r[1]), "selectionRange": Rng(d.Text, sel[0], sel[1])}
		ch := []any{}
		for _, c := range sy.Children {
			ch = append(ch, conv(c))
		}
		m["children"] = ch
		return m
	}
	out := []any{}
	for _, g := range d.an.Globals {
		if g.Kind == "variant" {
			continue
		}
		out = append(out, conv(g))
	}
	return out
}

// ---------- semantic tokens ----------
func semIdx(name string) int {
	for i, n := range semTypes {
		if n == name {
			return i
		}
	}
	return 6
}

type semTok struct{ pos, end, typ, mod int }

// SemanticSpans classifies every lexical element (also used directly by the native Studio renderer).
func SemanticSpans(an *Analysis) []semTok {
	text := an.Text
	var out []semTok
	symType := map[string]string{"module": "namespace", "entity": "class", "type": "enum", "variant": "enumMember", "param": "parameter", "local": "variable", "loopvar": "variable", "global": "variable", "field": "property", "fn": "function", "action": "method", "flow": "event", "view": "namespace"}
	prev := 0
	gapComments := func(a, b int) {
		g := text[a:b]
		base := a
		for _, ln := range strings.SplitAfter(g, "\n") {
			if k := strings.Index(ln, "//"); k >= 0 {
				e := strings.TrimRight(ln, "\n")
				out = append(out, semTok{base + k, base + len(e), semIdx("comment"), 0})
			}
			base += len(ln)
		}
	}
	occ := map[int]Occ{}
	for _, o := range an.Occurrences() {
		occ[o.Pos] = o
	}
	for i, t := range an.Toks {
		if t.Pos > prev {
			gapComments(prev, t.Pos)
		}
		prev = t.End
		switch t.Kind {
		case "kw":
			if t.S == "true" || t.S == "false" || t.S == "nil" {
				out = append(out, semTok{t.Pos, t.End, semIdx("number"), 0})
			} else {
				out = append(out, semTok{t.Pos, t.End, semIdx("keyword"), 0})
			}
		case "num":
			out = append(out, semTok{t.Pos, t.End, semIdx("number"), 0})
		case "op":
			if t.S != "{" && t.S != "}" && t.S != "(" && t.S != ")" && t.S != "," && t.S != "[" && t.S != "]" {
				out = append(out, semTok{t.Pos, t.End, semIdx("operator"), 0})
			}
		case "str":
			// split around interpolated identifiers
			p := t.Pos
			for _, o := range an.Occurrences() {
				if o.Pos >= t.Pos && o.End <= t.End {
					if o.Pos > p {
						out = append(out, semTok{p, o.Pos, semIdx("string"), 0})
					}
					out = append(out, classify(an, o, symType))
					p = o.End
				}
			}
			if p < t.End {
				out = append(out, semTok{p, t.End, semIdx("string"), 0})
			}
		case "id":
			o, ok := occ[t.Pos]
			if !ok {
				o = Occ{t.Pos, t.End, i > 0 && an.Toks[i-1].S == "."}
			}
			out = append(out, classify(an, o, symType))
		}
	}
	if prev < len(text) {
		gapComments(prev, len(text))
	}
	return out
}
func classify(an *Analysis, o Occ, symType map[string]string) semTok {
	name := an.Text[o.Pos:o.End]
	if s, ok := an.ByTok[o.Pos]; ok {
		return semTok{o.Pos, o.End, semIdx(symType[s.Kind]), 1}
	}
	if s := an.Resolve(name, o.Pos, o.Prev); s != nil {
		return semTok{o.Pos, o.End, semIdx(symType[s.Kind]), 0}
	}
	if o.Prev {
		return semTok{o.Pos, o.End, semIdx("property"), 0}
	}
	if builtinTypeNames[name] {
		return semTok{o.Pos, o.End, semIdx("type"), 2}
	}
	if builtinNames[name] {
		return semTok{o.Pos, o.End, semIdx("function"), 2}
	}
	return semTok{o.Pos, o.End, semIdx("variable"), 0}
}

func (s *LSPServer) semTokens(d *lspDoc) []int {
	spans := SemanticSpans(d.an)
	data := []int{}
	pl, pc := 0, 0
	for _, sp := range spans {
		// split multi-line spans
		a := sp.pos
		for a < sp.end {
			e := sp.end
			if k := strings.IndexByte(d.Text[a:e], '\n'); k >= 0 {
				e = a + k
			}
			if e > a {
				st := OffToPos(d.Text, a)
				ln := utf16Len(d.Text[a:e])
				dl := st.Line - pl
				dc := st.Character
				if dl == 0 {
					dc -= pc
				}
				data = append(data, dl, dc, ln, sp.typ, sp.mod)
				pl, pc = st.Line, st.Character
			}
			a = e + 1
		}
	}
	return data
}

// ---------- code actions ----------
var didYouMean = regexp.MustCompile(`did you mean '([^']+)'`)

func (s *LSPServer) codeActions(d *lspDoc, r LRange, diags []map[string]any) []any {
	out := []any{}
	for _, dg := range diags {
		data, _ := dg["data"].(map[string]any)
		h, _ := data["hint"].(string)
		if m := didYouMean.FindStringSubmatch(h); m != nil {
			rb, _ := json.Marshal(dg["range"])
			var rr LRange
			json.Unmarshal(rb, &rr)
			out = append(out, map[string]any{"title": "改为 '" + m[1] + "'", "kind": "quickfix", "diagnostics": []any{dg}, "isPreferred": true,
				"edit": map[string]any{"changes": map[string]any{d.URI: []any{map[string]any{"range": rr, "newText": m[1]}}}}})
		}
	}
	// agent patch proposals as code actions
	prompts := []string{}
	_, ds := CheckSourceIn(d.Text, d.dir)
	for _, x := range ds {
		if x["severity"] == "error" || x["hint"] != nil {
			prompts = append(prompts, "修复错误")
			break
		}
	}
	off := PosToOff(d.Text, r.Start)
	for _, g := range d.an.Globals {
		if g.Kind == "entity" && off >= g.Range[0] && off <= g.Range[1] {
			prompts = append(prompts, "给 "+g.Name+" 添加字段 note: Text")
		}
	}
	for _, pr := range prompts {
		p := Propose(d.Text, pr)
		if len(p.Ops) == 0 || p.Source == d.Text {
			continue
		}
		d.nextProp++
		d.props[d.nextProp] = &p
		st := "通过类型检查"
		if !p.OK {
			st = fmt.Sprintf("补丁后仍有 %d 个问题", len(p.Diags))
		}
		kind := "refactor.rewrite"
		if pr == "修复错误" {
			kind = "quickfix"
		}
		out = append(out, map[string]any{"title": "智能体补丁：" + p.Summary + "（" + st + "）", "kind": kind,
			"edit":    fullEdit(d.URI, d.Text, p.Source),
			"command": map[string]any{"title": "记录到历史", "command": "aether.acceptProposal", "arguments": []any{d.URI, d.nextProp}}})
	}
	return out
}

func contractsOf(src string) []any {
	out := []any{}
	prog, err := Parse(src)
	if err != nil {
		return out
	}
	for _, d := range prog["decls"].([]any) {
		dn := d.(Node)
		nm, _ := dn["name"].(string)
		for _, kk := range [][2]string{{"invariants", "invariant"}, {"requires", "requires"}, {"ensures", "ensures"}} {
			if l, ok := dn[kk[0]].([]any); ok {
				for _, e := range l {
					out = append(out, map[string]any{"kind": kk[1], "owner": nm, "text": Unparse(e.(Node)), "line": ln(e.(Node))})
				}
			}
		}
		if al, ok := dn["allow"].([]any); ok && len(al) > 0 {
			out = append(out, map[string]any{"kind": "allow", "owner": nm, "text": strings.Join(strs(al), ", "), "line": ln(dn)})
		}
	}
	return out
}

func runLSP() int {
	s := NewLSP(os.Stdout)
	if err := s.Serve(os.Stdin); err != nil && err != io.EOF {
		fmt.Fprintln(os.Stderr, "aether lsp:", err)
		return 1
	}
	return 0
}

func suggestName(an *Analysis, bad string) string {
	best, bd := "", 3
	try := func(n string) {
		if d := editDist(bad, n); d < bd && (d <= 1 || (d == 2 && len([]rune(bad)) >= 4)) {
			best, bd = n, d
		}
	}
	for _, g := range an.Globals {
		try(g.Name)
	}
	for _, l := range an.Locals {
		try(l.Name)
	}
	for b := range builtinNames {
		try(b)
	}
	return best
}
func editDist(a, b string) int {
	x, y := []rune(a), []rune(b)
	prev := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(x); i++ {
		cur := make([]int, len(y)+1)
		cur[0] = i
		for j := 1; j <= len(y); j++ {
			c := 1
			if x[i-1] == y[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev = cur
	}
	return prev[len(y)]
}
