package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const Version = "0.6.0"

type offscreen struct {
	w, h int
	ev   chan Event
}

func (o *offscreen) Present(*image.RGBA) {}
func (o *offscreen) Events() chan Event  { return o.ev }
func (o *offscreen) Size() (int, int)    { return o.w, o.h }

const usage = `Aether ` + Version + ` — native runtime (no Python, no browser required)

usage: aether <command> [args]
  run <file> [--db F]        execute a program (in-memory unless --db)
  check <file> [--json]      static checks (types, effects, contracts, views)
  test <file>... [--json]    run inline test / example blocks
  ui <file> [--db F] [--tui] native desktop window for the app (TUI with --tui)
  serve <file> [--port 8080] [--db F]   HTTP/JSON API (+ optional web page) for interop
  studio [file] [--tui] [--api 127.0.0.1:7777]   native Studio IDE (+ agent HTTP API)
  flow <file> <name> [--db F] execute a flow
  explain <file> | explain <CODE>   natural-language projection / 错误码中文解释 (E200, W300, M401...)
  migrate <file> [--db F] [--json] [--apply] [--drop]   schema vs data plan (M400-M404), then apply
  openapi <file>             OpenAPI 3.1 (with entity schemas) for the generated HTTP API
  agents <file> [-w]         generate AGENTS.md (agents.md format) from the program
  mcp                        Model Context Protocol server on stdio (read-only tools for agents)
  errors [--json]            list all diagnostic codes
  ast <file> | fmt <file> [-w] | hash <file>
  ops <file> <json|@file> [-w --author A --reason R]   structured agent edits
  propose <file> <prompt>    ask the agent (LLM via AETHER_LLM_API_KEY, else mock)
  history|undo|redo <file>   content-addressed change history
  lsp                        Language Server Protocol server on stdio (VS Code / Aether IDE)
  conform <golden.json>      run the conformance suite
  new <dir> [--template blank|todo|shop|blog|crm]   new project from a template (aether templates)
  mvc <file> [--json]        Model / Controller / View architecture summary
  compact <file>             minimal-token canonical form for agents (lossless; comments dropped)
  expand <file>              compact form -> canonical human text (types restored, formatted)
  skeleton <file>            signature-level summary for context windows
  context <file> <symbol> [--json]  the slice an agent needs: decl + deps + contracts + users + skeleton
  deploy <file> [--target bundle|local] [--port 8080] [--out DIR]   deployment bundle (+ Dockerfile)
  flow runs <file> | flow resume <file> <id>   durable flow runs
  pkg init|add|install|remove|list   package manager (aether.json / aether.lock / aether_modules)
  dap                        Debug Adapter Protocol server on stdio (debugger)
  doctor                     environment self-check (OS, PATH, writable dirs, hello)
  version`

func readSrc(p string) (string, string, error) {
	b, err := os.ReadFile(p)
	abs, _ := filepath.Abs(p)
	if err != nil {
		return "", filepath.Dir(abs), err
	}
	// Normalize CRLF/CR so Windows-edited .ae files parse the same as LF.
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s, filepath.Dir(abs), nil
}
func printErr(file string, err error) int {
	switch e := err.(type) {
	case *AeThrow:
		fmt.Fprintln(os.Stderr, "uncaught error: "+show(e.V))
	case *AeError:
		fmt.Fprintf(os.Stderr, "%s:%d:%d: error %s: %s\n", file, e.Line, e.Col, e.Code, e.Msg)
	default:
		fmt.Fprintln(os.Stderr, err)
	}
	return 1
}
func defaultDB(file string) string { return strings.TrimSuffix(file, filepath.Ext(file)) + ".aedb" }

func runCLI(args []string) int {
	if len(args) == 0 {
		fmt.Println(usage)
		return 2
	}
	cmd := args[0]
	if cmd == "lsp" {
		return runLSP()
	}
	if cmd == "mcp" {
		ServeMCP(os.Stdin, os.Stdout)
		return 0
	}
	if cmd == "dap" {
		return runDAP()
	}
	if cmd == "pkg" {
		return cmdPkg(args[1:])
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	write := fs.Bool("w", false, "")
	db := fs.String("db", "", "")
	port := fs.Int("port", 8080, "")
	host := fs.String("host", "127.0.0.1", "")
	tui := fs.Bool("tui", false, "")
	api := fs.String("api", "127.0.0.1:7777", "")
	script := fs.String("script", os.Getenv("AETHER_SCRIPT"), "")
	offs := fs.String("offscreen", "", "")
	author := fs.String("author", "agent", "")
	reason := fs.String("reason", "", "")
	size := fs.String("size", "1440x900", "")
	tplF := fs.String("template", "blank", "")
	outF := fs.String("out", "", "")
	targetF := fs.String("target", "bundle", "")
	applyF := fs.Bool("apply", false, "")
	dropF := fs.Bool("drop", false, "")
	// allow flags after positionals
	var pos []string
	rest := args[1:]
	for len(rest) > 0 {
		fs.Parse(rest)
		rest = fs.Args()
		if len(rest) > 0 {
			pos = append(pos, rest[0])
			rest = rest[1:]
		}
	}
	need := func(n int) bool {
		if len(pos) < n {
			fmt.Fprintln(os.Stderr, usage)
			return false
		}
		return true
	}
	switch cmd {
	case "version", "--version", "-v":
		fmt.Println("aether", Version, "(native)")
		return 0
	case "doctor":
		return cmdDoctor()
	case "conform":
		if !need(1) {
			return 2
		}
		return runConform(pos[0])
	case "help", "-h", "--help":
		fmt.Println(usage)
		return 0
	case "new":
		return cmdNew(pos, *tplF)
	case "errors":
		if *asJSON {
			b, _ := json.MarshalIndent(ErrCodesSorted(), "", " ")
			fmt.Println(string(b))
			return 0
		}
		for _, d := range ErrCodesSorted() {
			fmt.Printf("%s\t%s\n", d.Code, d.Title)
		}
		return 0
	case "templates":
		for _, t := range Templates {
			fmt.Printf("%s\t%s\t%s\n", t.ID, t.Title, t.Desc)
		}
		return 0
	case "deploy":
		if !need(1) {
			return 2
		}
		p := 8080
		if *port != 8080 {
			p = *port
		}
		return cmdDeploy(pos[0], *outF, *targetF, p)
	case "compact", "expand", "skeleton":
		if !need(1) {
			return 2
		}
		src, _, err := readSrc(pos[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		f := map[string]func(string) (string, error){"compact": Compact, "expand": Expand, "skeleton": Skeleton}[cmd]
		out, err := f(src)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(out)
		return 0
	case "context":
		if len(pos) < 2 {
			fmt.Fprintln(os.Stderr, "用法: aether context <file.ae> <symbol> [--json]")
			return 2
		}
		src, dir, err := readSrc(pos[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		r, err := AgentContext(src, pos[1], dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if *asJSON {
			fmt.Println(toJSON(r))
		} else {
			fmt.Print(r["text"])
		}
		return 0
	case "mvc":
		if !need(1) {
			return 2
		}
		return cmdMVC(pos[0], *asJSON)
	case "test":
		if !need(1) {
			return 2
		}
		fails, total := 0, 0
		all := map[string]any{}
		for _, f := range pos {
			src, dir, err := readSrc(f)
			if err != nil {
				return printErr(f, err)
			}
			it := NewInterp(":memory:", false)
			if err := it.Load(src, dir); err != nil {
				return printErr(f, err)
			}
			res := it.RunTests()
			all[f] = testsAny(res)
			for _, r := range res {
				total++
				if !r.OK {
					fails++
				}
				if !*asJSON {
					m := "PASS"
					if !r.OK {
						m = "FAIL"
					}
					msg := ""
					if r.Message != "" {
						msg = "  — " + r.Message
					}
					fmt.Printf("%s %s:%d %s%s\n", m, f, r.Line, r.Name, msg)
				}
			}
		}
		if *asJSON {
			fmt.Println(toJSON(all))
		} else {
			fmt.Printf("\n%d passed, %d failed\n", total-fails, fails)
		}
		if fails > 0 {
			return 1
		}
		return 0
	case "studio":
		path := ""
		if len(pos) > 0 {
			path = pos[0]
		}
		if *tui {
			if err := runTuiStudio(path); err != nil {
				return printErr(path, err)
			}
			return 0
		}
		var win Window
		var err error
		w, h := 1440, 900
		fmt.Sscanf(*size, "%dx%d", &w, &h)
		if *offs != "" {
			fmt.Sscanf(*offs, "%dx%d", &w, &h)
			win = &offscreen{w, h, make(chan Event)}
		} else if win, err = openWindow("Aether Studio", w, h); err != nil {
			fmt.Fprintln(os.Stderr, "native window unavailable ("+err.Error()+"); starting terminal Studio")
			if err := runTuiStudio(path); err != nil {
				return printErr(path, err)
			}
			return 0
		}
		if path != "" {
			if _, err := os.Stat(path); err != nil {
				return printErr(path, err)
			}
		}
		runGuiStudioWithAPI(path, win, *script, *api)
		return 0
	}
	if !need(1) {
		return 2
	}
	if cmd == "flow" && len(pos) > 1 && (pos[0] == "runs" || pos[0] == "resume") {
		pos = append([]string{pos[1], pos[0]}, pos[2:]...) // flow runs f / flow resume f id -> f runs [id]
	}
	if cmd == "explain" && IsErrCode(pos[0]) {
		if t, ok := ExplainCode(pos[0]); ok {
			fmt.Print(t)
			return 0
		}
		fmt.Fprintln(os.Stderr, "未知错误码："+pos[0]+"（aether errors 列出全部）")
		return 2
	}
	file := pos[0]
	src, dir, err := readSrc(file)
	if err != nil {
		return printErr(file, err)
	}
	histPath := file + ".history.json"
	switch cmd {
	case "run":
		d := ":memory:"
		if *db != "" {
			d = *db
		}
		it := NewInterp(d, true)
		defer it.Store.Close()
		if err := it.Load(src, dir); err != nil {
			return printErr(file, err)
		}
		return 0
	case "check":
		_, d := CheckSourceIn(src, dir)
		for _, x := range d {
			enrichDiag(x)
		}
		if *asJSON {
			b, _ := json.MarshalIndent(d, "", " ")
			fmt.Println(string(b))
		} else {
			for _, x := range d {
				h := ""
				if s, ok := x["hint"].(string); ok {
					h = "  (hint: " + s + ")"
				}
				fmt.Printf("%s:%v: %s %s: %s%s\n", file, x["line"], x["severity"], x["code"], x["message"], h)
				if ln, ok := x["line"].(int); ok && ln > 0 {
					if ls := strings.Split(src, "\n"); ln <= len(ls) {
						fmt.Printf("  %4d │ %s\n", ln, strings.TrimRight(ls[ln-1], "\r"))
					}
				}
				if c, ok := x["code"].(string); ok {
					if ed, ok := ErrDocs[c]; ok {
						fmt.Printf("    %s；详见 aether explain %s\n", ed.Title, c)
					}
				}
			}
			if len(d) == 0 {
				fmt.Println(file + ": ok")
			}
		}
		for _, x := range d {
			if x["severity"] == "error" {
				return 1
			}
		}
		return 0
	case "ast":
		p, err := Parse(src)
		if err != nil {
			return printErr(file, err)
		}
		b, _ := json.MarshalIndent(p, "", " ")
		fmt.Println(string(b))
		return 0
	case "fmt":
		out, err := Fmt(src)
		if err != nil {
			return printErr(file, err)
		}
		if *write {
			os.WriteFile(file, []byte(out), 0644)
		} else {
			fmt.Print(out)
		}
		return 0
	case "hash":
		fmt.Println(ASTHash(src))
		return 0
	case "explain":
		t, err := Explain(src)
		if err != nil {
			return printErr(file, err)
		}
		fmt.Println(t)
		return 0
	case "flow":
		sub := ""
		if len(pos) > 1 && (pos[1] == "runs" || pos[1] == "resume") {
			sub = pos[1]
			pos = append([]string{pos[0]}, pos[2:]...)
		}
		if sub != "runs" && !need(2) {
			return 2
		}
		d := *db
		if d == "" {
			d = defaultDB(file)
		}
		it := NewInterp(d, false)
		if err := it.Load(src, dir); err != nil {
			return printErr(file, err)
		}
		var r *Record
		if sub == "runs" {
			it.RecoverFlows()
			fmt.Println(toJSON(it.FlowRuns()))
			return 0
		}
		if err := catch(func() {
			if sub == "resume" {
				id, _ := strconv.ParseInt(pos[1], 10, 64)
				r = it.ResumeFlow(id)
			} else {
				r = it.RunFlow(pos[1], nil)
			}
		}); err != nil {
			return printErr(file, err)
		}
		fmt.Println(toJSON(r))
		if !truthy(r.M["ok"]) {
			return 1
		}
		return 0
	case "agents":
		t, err := AgentsMD(src, filepath.Base(file))
		if err != nil {
			return printErr(file, err)
		}
		if *write {
			out := filepath.Join(filepath.Dir(file), "AGENTS.md")
			if err := os.WriteFile(out, []byte(t), 0644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			fmt.Println("已写入 " + out)
			return 0
		}
		fmt.Print(t)
		return 0
	case "openapi":
		it := NewInterp(":memory:", false)
		if err := it.Load(src, dir); err != nil {
			return printErr(file, err)
		}
		b, _ := json.MarshalIndent(BuildOpenAPI(it), "", " ")
		fmt.Println(string(b))
		return 0
	case "migrate":
		d := *db
		if d == "" {
			d = defaultDB(file)
		}
		it := NewInterp(d, false)
		defer it.Store.Close()
		if err := it.Load(src, dir); err != nil {
			return printErr(file, err)
		}
		return cmdMigrate(it, *asJSON, *applyF, *dropF)
	case "serve":
		d := *db
		if d == "" {
			d = defaultDB(file)
		}
		app, err := NewApp(src, d, dir)
		if err != nil {
			return printErr(file, err)
		}
		addr := fmt.Sprintf("%s:%d", *host, *port)
		fmt.Printf("Aether app '%s' -> http://%s  (API: /api/_meta, /api/_openapi, storage: %s [AeStore WAL])\n", app.It.Module, addr, d)
		srv := &http.Server{Addr: addr, Handler: httpHandler(app.Handle)}
		return serveUntilSignal(srv, app)
	case "ui":
		d := *db
		if d == "" {
			d = defaultDB(file)
		}
		app, err := NewApp(src, d, dir)
		if err != nil {
			return printErr(file, err)
		}
		if *tui {
			runTuiApp(app)
			return 0
		}
		w, h := 1100, 760
		fmt.Sscanf(*size, "%dx%d", &w, &h)
		if *size == "1440x900" {
			w, h = 1100, 760
		}
		var win Window
		if *offs != "" {
			fmt.Sscanf(*offs, "%dx%d", &w, &h)
			win = &offscreen{w, h, make(chan Event)}
		} else if win, err = openWindow("Aether · "+app.It.Module, w, h); err != nil {
			fmt.Fprintln(os.Stderr, "native window unavailable ("+err.Error()+"); using terminal UI")
			runTuiApp(app)
			return 0
		}
		runAppWindow(app, win, *script)
		return 0
	case "ops", "propose":
		if !need(2) {
			return 2
		}
		var res map[string]any
		ok := false
		if cmd == "ops" {
			raw := pos[1]
			if strings.HasPrefix(raw, "@") {
				b, _ := os.ReadFile(raw[1:])
				raw = string(b)
			}
			var ops []map[string]any
			if err := json.Unmarshal([]byte(raw), &ops); err != nil {
				return printErr(file, err)
			}
			r, err := ApplyOps(src, ops)
			if err != nil {
				return printErr(file, err)
			}
			ok = r.OK
			res = map[string]any{"diagnostics": diagList(r.Diags), "ok": r.OK, "diff": r.Diff}
			if *write && r.OK {
				h := OpenHistory(histPath)
				if len(h.Entries) == 0 {
					h.Commit(src, "human", "baseline", nil, "baseline")
				}
				rs := *reason
				if rs == "" {
					rs = "apply ops"
				}
				e := h.Commit(r.Source, *author, rs, ops, "ops")
				res["history"] = map[string]any{"id": e.ID, "hash": e.Hash, "author": e.Author}
				os.WriteFile(file, []byte(r.Source), 0644)
			}
		} else {
			p := Propose(src, pos[1])
			ok = p.OK
			res = p.JSON()
			delete(res, "source")
		}
		b, _ := json.MarshalIndent(res, "", " ")
		fmt.Println(string(b))
		if ok {
			return 0
		}
		return 1
	case "history":
		h := OpenHistory(histPath)
		for i, e := range h.Entries {
			m := " "
			if i == h.Pos {
				m = "→"
			}
			u := ""
			if i > h.Pos {
				u = " (undone)"
			}
			fmt.Printf("%s #%d %s %s [%s] %s: %s%s\n", m, e.ID, e.Hash, e.Time, e.Author, e.Kind, e.Reason, u)
		}
		if len(h.Entries) == 0 {
			fmt.Println("(no history yet)")
		}
		return 0
	case "undo", "redo":
		h := OpenHistory(histPath)
		var s string
		var err error
		if cmd == "undo" {
			s, err = h.Undo()
		} else {
			s, err = h.Redo()
		}
		if err != nil {
			return printErr(file, err)
		}
		os.WriteFile(file, []byte(s), 0644)
		fmt.Printf("%s: now at %s (%s)\n", cmd, h.Head().Hash, h.Head().Reason)
		return 0
	}
	fmt.Fprintln(os.Stderr, usage)
	return 2
}

func runGuiStudioWithAPI(path string, win Window, script, api string) {
	runGuiStudioAPI = api
	runGuiStudio(path, win, script)
}

var runGuiStudioAPI string

func serveUntilSignal(srv *http.Server, app *App) int {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case <-sig:
		fmt.Fprintln(os.Stderr, "\nshutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		if app != nil && app.It != nil && app.It.Store != nil {
			app.It.Store.MaybeCompact(true)
			_ = app.It.Store.Close()
		}
	}
	return 0
}
