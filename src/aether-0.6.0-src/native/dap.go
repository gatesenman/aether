package main

// `aether dap`: Debug Adapter Protocol server on stdio (VS Code / Aether IDE debugger, v0.5).
// Breakpoints, continue, step over/in/out, pause, call stack, scopes/variables (records & lists
// expandable), evaluate (watch / hover / debug console). Launch modes: run (top level), test, flow.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type DbgFrame struct {
	Name string
	Env  *Env
	Line int
}

type dapServer struct {
	in                  *bufio.Reader
	out                 io.Writer
	wmu                 sync.Mutex
	seq                 int
	mu                  sync.Mutex
	bps                 map[int]bool
	program             string
	it                  *Interp
	resume              chan string
	mode                string // continue | next | stepIn | stepOut | pause
	refDepth, refLine   int
	lastLine, lastDepth int
	vars                map[int]Value
	nextRef             int
	stopped             bool
	pauseReq            bool
	stopOnEntry         bool
	entryDone           bool
	launch              map[string]any
	configured          chan bool
}

func runDAP() int {
	s := &dapServer{in: bufio.NewReaderSize(os.Stdin, 1<<16), out: os.Stdout, bps: map[int]bool{}, resume: make(chan string), configured: make(chan bool, 1)}
	for {
		msg, err := readLSP(s.in)
		if err != nil {
			return 0
		}
		var req struct {
			Seq       int             `json:"seq"`
			Command   string          `json:"command"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(msg, &req) != nil {
			continue
		}
		body, err := s.handle(req.Command, req.Arguments)
		resp := map[string]any{"type": "response", "request_seq": req.Seq, "command": req.Command, "success": err == nil}
		if err != nil {
			resp["message"] = err.Error()
		} else if body != nil {
			resp["body"] = body
		}
		s.send(resp)
		if req.Command == "initialize" {
			s.event("initialized", nil)
		}
		if req.Command == "disconnect" {
			return 0
		}
	}
}

func readLSP(r *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, _ = strconv.Atoi(strings.TrimSpace(line[15:]))
		}
	}
	if n < 0 {
		return nil, fmt.Errorf("no length")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

func (s *dapServer) send(m map[string]any) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.seq++
	m["seq"] = s.seq
	b, _ := json.Marshal(m)
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(b), b)
}
func (s *dapServer) event(name string, body any) {
	m := map[string]any{"type": "event", "event": name}
	if body != nil {
		m["body"] = body
	}
	s.send(m)
}

func (s *dapServer) depth() int { return len(s.it.Frames) }

// hook runs on the interpreter goroutine before every statement.
func (s *dapServer) hook(st Node, env *Env) {
	line := ln(st)
	if line <= 0 {
		return
	}
	top := s.it.Frames[len(s.it.Frames)-1]
	top.Line, top.Env = line, env
	d := s.depth()
	s.mu.Lock()
	stop, reason := false, ""
	newLine := line != s.lastLine || d != s.lastDepth
	switch {
	case s.stopOnEntry && !s.entryDone:
		stop, reason = true, "entry"
		s.entryDone = true
	case s.pauseReq:
		stop, reason = true, "pause"
		s.pauseReq = false
	case s.bps[line] && newLine:
		stop, reason = true, "breakpoint"
	case s.mode == "stepIn" && newLine:
		stop, reason = true, "step"
	case s.mode == "next" && ((d <= s.refDepth && line != s.refLine) || d < s.refDepth):
		stop, reason = true, "step"
	case s.mode == "stepOut" && d < s.refDepth:
		stop, reason = true, "step"
	}
	s.lastLine, s.lastDepth = line, d
	if !stop {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.vars = map[int]Value{}
	s.nextRef = 1000
	s.mu.Unlock()
	s.event("stopped", map[string]any{"reason": reason, "threadId": 1, "allThreadsStopped": true})
	mode := <-s.resume
	s.mu.Lock()
	s.mode, s.refDepth, s.refLine, s.stopped = mode, d, line, false
	s.mu.Unlock()
}

func (s *dapServer) run() {
	src, err := os.ReadFile(s.program)
	if err != nil {
		s.event("output", map[string]any{"category": "stderr", "output": err.Error() + "\n"})
		s.event("terminated", nil)
		return
	}
	it := NewInterp(":memory:", false)
	s.it = it
	it.Frames = []*DbgFrame{{Name: "main", Env: it.Globals}}
	it.OnEmit = func(t string) { s.event("output", map[string]any{"category": "stdout", "output": t + "\n"}) }
	<-s.configured
	it.Hook = s.hook
	code := 0
	mode, _ := s.launch["mode"].(string)
	err = it.Load(string(src), filepath.Dir(s.program))
	if err == nil && mode == "test" {
		for _, r := range it.RunTests() {
			m := "PASS"
			if !r.OK {
				m, code = "FAIL", 1
			}
			s.event("output", map[string]any{"category": "console", "output": fmt.Sprintf("%s %s %s\n", m, r.Name, r.Message)})
		}
	}
	if err == nil && mode == "flow" {
		fl, _ := s.launch["flow"].(string)
		var r *Record
		if e := catch(func() { r = it.RunFlow(fl, nil) }); e != nil {
			err = e
		} else {
			s.event("output", map[string]any{"category": "console", "output": "flow " + fl + ": " + toJSON(r) + "\n"})
		}
	}
	if err != nil {
		code = 1
		msg := err.Error()
		if e, ok := err.(*AeError); ok {
			msg = fmt.Sprintf("%s:%d: %s %s", filepath.Base(s.program), e.Line, e.Code, e.Msg)
		} else if t, ok := err.(*AeThrow); ok {
			msg = "uncaught: " + show(t.V)
		}
		s.event("output", map[string]any{"category": "stderr", "output": msg + "\n"})
	}
	s.event("exited", map[string]any{"exitCode": code})
	s.event("terminated", nil)
}

func (s *dapServer) ref(v Value) int {
	switch v.(type) {
	case *Record, *List, *Variant:
		s.nextRef++
		s.vars[s.nextRef] = v
		return s.nextRef
	}
	return 0
}
func (s *dapServer) varOf(name string, v Value) map[string]any {
	return map[string]any{"name": name, "value": show(v), "type": typename(v), "variablesReference": s.ref(v)}
}

func (s *dapServer) envVars(e *Env, stopAt *Env) []any {
	seen := map[string]bool{}
	out := []any{}
	for x := e; x != nil && x != stopAt; x = x.parent {
		x.mu.RLock()
		names := []string{}
		for k := range x.vars {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			v := x.vars[k]
			if seen[k] {
				continue
			}
			seen[k] = true
			switch v.(type) {
			case *Builtin, *Closure, *Ctor, *EntityRef:
				continue
			}
			out = append(out, s.varOf(k, v))
		}
		x.mu.RUnlock()
	}
	return out
}

type dapArgs struct {
	Source      struct{ Path string } `json:"source"`
	Breakpoints []struct {
		Line int `json:"line"`
	} `json:"breakpoints"`
	Program            string `json:"program"`
	StopOnEntry        bool   `json:"stopOnEntry"`
	FrameID            int    `json:"frameId"`
	VariablesReference int    `json:"variablesReference"`
	Expression         string `json:"expression"`
}

func (s *dapServer) handle(cmd string, raw json.RawMessage) (any, error) {
	var a dapArgs
	json.Unmarshal(raw, &a)
	switch cmd {
	case "initialize":
		return map[string]any{"supportsConfigurationDoneRequest": true, "supportsEvaluateForHovers": true, "supportsTerminateRequest": true}, nil
	case "launch":
		var m map[string]any
		json.Unmarshal(raw, &m)
		s.launch = m
		s.program = a.Program
		s.stopOnEntry = a.StopOnEntry
		go s.run()
		return nil, nil
	case "setBreakpoints":
		s.mu.Lock()
		s.bps = map[int]bool{}
		res := []any{}
		for _, b := range a.Breakpoints {
			s.bps[b.Line] = true
			res = append(res, map[string]any{"verified": true, "line": b.Line})
		}
		s.mu.Unlock()
		return map[string]any{"breakpoints": res}, nil
	case "setExceptionBreakpoints":
		return map[string]any{"breakpoints": []any{}}, nil
	case "configurationDone":
		select {
		case s.configured <- true:
		default:
		}
		return nil, nil
	case "threads":
		return map[string]any{"threads": []any{map[string]any{"id": 1, "name": "main"}}}, nil
	case "stackTrace":
		fr := []any{}
		if s.it != nil {
			for i := len(s.it.Frames) - 1; i >= 0; i-- {
				f := s.it.Frames[i]
				fr = append(fr, map[string]any{"id": i + 1, "name": f.Name, "line": f.Line, "column": 1, "source": map[string]any{"path": s.program, "name": filepath.Base(s.program)}})
			}
		}
		return map[string]any{"stackFrames": fr, "totalFrames": len(fr)}, nil
	case "scopes":
		i := a.FrameID - 1
		if s.it == nil || i < 0 || i >= len(s.it.Frames) {
			return map[string]any{"scopes": []any{}}, nil
		}
		return map[string]any{"scopes": []any{
			map[string]any{"name": "局部变量", "variablesReference": a.FrameID, "expensive": false},
			map[string]any{"name": "全局", "variablesReference": 999, "expensive": false}}}, nil
	case "variables":
		r := a.VariablesReference
		s.mu.Lock()
		defer s.mu.Unlock()
		if r == 999 {
			return map[string]any{"variables": s.envVars(s.it.Globals, nil)}, nil
		}
		if r >= 1 && r <= len(s.it.Frames) {
			f := s.it.Frames[r-1]
			if f.Env == s.it.Globals {
				return map[string]any{"variables": []any{}}, nil
			}
			return map[string]any{"variables": s.envVars(f.Env, s.it.Globals)}, nil
		}
		out := []any{}
		switch v := s.vars[r].(type) {
		case *Record:
			for _, k := range v.Keys {
				out = append(out, s.varOf(k, v.M[k]))
			}
		case *List:
			for i, x := range v.Items {
				out = append(out, s.varOf(strconv.Itoa(i), x))
			}
		case *Variant:
			for i, x := range v.Vals {
				n := strconv.Itoa(i)
				if i < len(v.C.Fields) {
					n = v.C.Fields[i]
				}
				out = append(out, s.varOf(n, x))
			}
		}
		return map[string]any{"variables": out}, nil
	case "evaluate":
		if s.it == nil {
			return nil, fmt.Errorf("not running")
		}
		env := s.it.Globals
		if i := a.FrameID - 1; i >= 0 && i < len(s.it.Frames) {
			env = s.it.Frames[i].Env
		}
		prog, err := Parse(a.Expression)
		if err != nil {
			return nil, err
		}
		ds := prog["decls"].([]any)
		if len(ds) != 1 || ds[0].(Node)["k"] != "ExprStmt" {
			return nil, fmt.Errorf("只能计算表达式")
		}
		var v Value
		hook := s.it.Hook
		s.it.Hook = nil
		e := catch(func() { v = s.it.eval(ds[0].(Node)["expr"].(Node), env) })
		s.it.Hook = hook
		if e != nil {
			return nil, e
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return map[string]any{"result": show(v), "type": typename(v), "variablesReference": s.ref(v)}, nil
	case "continue", "next", "stepIn", "stepOut":
		s.mu.Lock()
		st := s.stopped
		s.mu.Unlock()
		if st {
			mode := cmd
			if cmd == "continue" {
				mode = "continue"
			}
			s.resume <- mode
		}
		if cmd == "continue" {
			return map[string]any{"allThreadsContinued": true}, nil
		}
		return nil, nil
	case "pause":
		s.mu.Lock()
		s.pauseReq = true
		s.mu.Unlock()
		return nil, nil
	case "disconnect", "terminate":
		s.mu.Lock()
		st := s.stopped
		s.bps = map[int]bool{}
		s.mode = "continue"
		s.mu.Unlock()
		if st {
			s.resume <- "continue"
		}
		if cmd == "terminate" {
			s.event("terminated", nil)
		}
		return nil, nil
	}
	return nil, nil
}
