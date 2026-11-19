package main

// Terminal UI fallback (any OS with a terminal): Studio editor + diagnostics + agent panel, and app views.

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

func runeW(r rune) int {
	if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a || (r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe4f) ||
		(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) || (r >= 0x1f300 && r <= 0x1faff)) {
		return 2
	}
	return 1
}
func strW(s string) int {
	n := 0
	for _, r := range s {
		n += runeW(r)
	}
	return n
}

// fit: cut/pad s to exactly w cells
func fit(s string, w int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r == '\t' {
			r = ' '
		}
		rw := runeW(r)
		if n+rw > w {
			break
		}
		b.WriteRune(r)
		n += rw
	}
	return b.String() + strings.Repeat(" ", w-n)
}

type keyReader struct{ r *bufio.Reader }

func (k *keyReader) next() Event {
	b, err := k.r.ReadByte()
	if err != nil {
		return Event{Type: EvClose}
	}
	switch {
	case b == 27:
		if k.r.Buffered() == 0 {
			return Event{Type: EvKey, Key: KEsc}
		}
		b2, _ := k.r.ReadByte()
		if b2 == '[' || b2 == 'O' {
			seq := ""
			for {
				c, _ := k.r.ReadByte()
				seq += string(c)
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
			m := map[string]int{"A": KUp, "B": KDown, "C": KRight, "D": KLeft, "H": KHome, "F": KEnd, "3~": KDelete, "5~": KPgUp, "6~": KPgDn, "1~": KHome, "4~": KEnd}
			return Event{Type: EvKey, Key: m[seq]}
		}
		return Event{Type: EvKey, Key: KEsc}
	case b == 13 || b == 10:
		return Event{Type: EvKey, Key: KEnter}
	case b == 9:
		return Event{Type: EvKey, Key: KTab}
	case b == 127 || b == 8:
		return Event{Type: EvKey, Key: KBackspace}
	case b < 27:
		return Event{Type: EvKey, Ctrl: true, Rune: rune('a' + b - 1)}
	case b < 0x80:
		return Event{Type: EvKey, Rune: rune(b)}
	}
	k.r.UnreadByte()
	r, _, _ := k.r.ReadRune()
	return Event{Type: EvKey, Rune: r}
}

func runTuiStudio(path string) error {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return fmt.Errorf("stdin is not a terminal")
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, old)
	out := bufio.NewWriter(os.Stdout)
	out.WriteString("\x1b[?1049h\x1b[?25l")
	defer func() { out.WriteString("\x1b[?25h\x1b[?1049l"); out.Flush() }()
	s := NewStudioCore(path)
	ed := &Editor{Anchor: -1}
	ed.Set(s.Src)
	tab := "diag"
	mode := "edit" // edit | prompt
	prompt := ""
	kr := &keyReader{bufio.NewReader(os.Stdin)}
	var lastProp *ChatMsg
	sync := func() {
		if ed.String() != s.Src {
			s.SetSource(ed.String())
			if s.Errors() == 0 {
				s.Commit("human", "人工编辑", nil, "edit", true)
			}
		}
	}
	for {
		W, H, err := term.GetSize(fd)
		if err != nil || W < 40 {
			W, H = 120, 36
		}
		lw := W * 55 / 100
		rw := W - lw - 1
		bodyH := H - 2
		ed.Scroll(bodyH)
		lines := ed.Lines()
		cl, cc := ed.LineCol()
		// right pane content
		var right []string
		title := map[string]string{"diag": "诊断", "tests": "测试", "exp": "解释", "hist": "历史", "agent": "智能体", "out": "输出", "app": "应用预览"}[tab]
		switch tab {
		case "diag":
			if len(s.Diags) == 0 {
				right = append(right, "[OK] 没有问题")
			}
			for _, d := range s.Diags {
				right = append(right, fmt.Sprintf("%s L%v %s", d["code"], d["line"], d["message"]))
			}
		case "out":
			right = append(right, s.Output...)
			if s.RunErr != "" {
				right = append(right, "[错误] "+s.RunErr)
			}
		case "tests":
			for _, t := range s.Tests {
				m := "PASS"
				if !t.OK {
					m = "FAIL"
				}
				right = append(right, m+" "+t.Name+" "+t.Message)
			}
		case "exp":
			t, _ := Explain(s.Src)
			for _, l := range strings.Split(t, "\n") {
				right = append(right, wrapW(strings.ReplaceAll(l, "**", ""), rw)...)
			}
		case "hist":
			for i := len(s.Hist.Entries) - 1; i >= 0; i-- {
				e := s.Hist.Entries[i]
				m := " "
				if i == s.Hist.Pos {
					m = "→"
				}
				right = append(right, fmt.Sprintf("%s#%d %s [%s] %s: %s", m, e.ID, e.Hash, e.Author, e.Kind, e.Reason))
			}
		case "agent":
			for _, m := range s.Chat {
				if m.Me {
					right = append(right, "› "+m.Text)
					continue
				}
				right = append(right, wrapW("[智能体] "+m.Prop.Summary, rw)...)
				for _, dl := range strings.Split(m.Prop.Diff, "\n") {
					right = append(right, dl)
				}
				switch m.Decision {
				case "":
					right = append(right, "[^O 接受] [^X 拒绝]")
				case "accepted":
					right = append(right, "[已接受]")
				default:
					right = append(right, "已拒绝")
				}
			}
			if len(s.Chat) == 0 {
				right = append(right, "按 ^A 输入需求，例如：给 Todo 添加字段 due: Text")
			}
			if len(right) > bodyH {
				right = right[len(right)-bodyH:]
			}
		case "app":
			right = renderAppText(s.Preview, rw)
		}
		var b strings.Builder
		b.WriteString("\x1b[H")
		hdr := fmt.Sprintf(" Aether Studio (TUI)  %s  #%s  错误:%d ", s.Path, s.Hash, s.Errors())
		b.WriteString("\x1b[48;5;54m\x1b[97m" + fit(hdr, lw) + "│" + fit(" "+title+"  [^D诊断 ^P预览 ^T测试 ^E解释 ^G历史 ^A智能体 ^R运行]", rw) + "\x1b[0m\r\n")
		for i := 0; i < bodyH-1; i++ {
			ln := ed.Top + i
			left := ""
			if ln < len(lines) {
				left = fmt.Sprintf("%3d ", ln+1) + lines[ln]
			}
			lf := fit(left, lw)
			if ln == cl && mode == "edit" {
				// draw cursor by inverting the char at column
				prefix := fmt.Sprintf("%3d ", ln+1) + string([]rune(lines[ln])[:cc])
				pw := strW(prefix)
				if pw < lw {
					rs := []rune(lf)
					// map cell pw to rune index
					cell, idx := 0, 0
					for idx < len(rs) && cell < pw {
						cell += runeW(rs[idx])
						idx++
					}
					if idx < len(rs) {
						lf = string(rs[:idx]) + "\x1b[7m" + string(rs[idx]) + "\x1b[0m" + string(rs[idx+1:])
					}
				}
			}
			r := ""
			if i < len(right) {
				r = right[i]
			}
			b.WriteString(lf + "\x1b[38;5;60m│\x1b[0m" + fit(" "+r, rw) + "\r\n")
		}
		status := " ^S保存 ^Z撤销 ^Y重做 ^L格式化 ^Q退出   " + s.Status
		if mode == "prompt" {
			status = " 智能体需求> " + prompt + "▏ (Enter 发送, Esc 取消)"
		}
		b.WriteString("\x1b[48;5;236m" + fit(status, W) + "\x1b[0m")
		out.WriteString(b.String())
		out.Flush()

		ev := kr.next()
		if ev.Type == EvClose {
			return nil
		}
		s.Status = ""
		if mode == "prompt" {
			switch {
			case ev.Key == KEnter:
				sync()
				lastProp = s.Ask(prompt)
				prompt, mode, tab = "", "edit", "agent"
			case ev.Key == KEsc:
				mode = "edit"
			case ev.Key == KBackspace:
				if r := []rune(prompt); len(r) > 0 {
					prompt = string(r[:len(r)-1])
				}
			case ev.Rune != 0 && !ev.Ctrl:
				prompt += string(ev.Rune)
			}
			continue
		}
		if ev.Ctrl {
			switch ev.Rune {
			case 'q':
				return nil
			case 's':
				sync()
				s.Status = s.Save()
			case 'z':
				s.Status = s.Undo()
				ed.Set(s.Src)
			case 'y':
				s.Status = s.Redo()
				ed.Set(s.Src)
			case 'r':
				sync()
				s.Run()
				tab = "out"
			case 't':
				sync()
				s.RunTests()
				tab = "tests"
			case 'e':
				sync()
				tab = "exp"
			case 'd':
				sync()
				tab = "diag"
			case 'g':
				tab = "hist"
			case 'p':
				sync()
				s.RebuildPreview()
				tab = "app"
			case 'a':
				mode, tab = "prompt", "agent"
			case 'o':
				if lastProp != nil {
					s.Accept(lastProp)
					ed.Set(s.Src)
				}
			case 'x':
				if lastProp != nil {
					s.Reject(lastProp)
				}
			case 'l':
				sync()
				if f, err := Fmt(s.Src); err == nil {
					s.SetSource(f)
					ed.Set(f)
				}
			}
			continue
		}
		ed.Key(ev)
		sync()
	}
}
func wrapW(s string, w int) []string {
	var out []string
	for strW(s) > w && w > 4 {
		rs := []rune(s)
		n, cells := 0, 0
		for n < len(rs) && cells+runeW(rs[n]) <= w {
			cells += runeW(rs[n])
			n++
		}
		out = append(out, string(rs[:n]))
		s = string(rs[n:])
	}
	return append(out, s)
}

// renderAppText: text projection of an app's views (used by TUI preview and `aether ui --tui`).
func renderAppText(app *App, w int) []string {
	if app == nil {
		return []string{"（无预览）"}
	}
	var out []string
	spec := app.Spec()
	for _, v := range spec["views"].([]any) {
		vm := v.(map[string]any)
		out = append(out, "━━ "+show(vm["name"])+" "+strings.Repeat("━", max(0, w-6-strW(show(vm["name"])))))
		for _, it := range vm["items"].([]any) {
			el := it.(map[string]any)
			switch el["kind"] {
			case "title":
				out = append(out, "  "+show(el["text"]))
			case "text", "section", "error":
				out = append(out, "  "+show(el["text"]))
			case "stat":
				out = append(out, fmt.Sprintf("  [%s: %s]", show(el["label"]), show(el["value"])))
			case "button":
				out = append(out, "  ( "+show(el["label"])+" → "+show(el["action"])+" )")
			case "form":
				out = append(out, "  ＋ 表单 "+show(el["entity"])+"   命令: add "+show(el["entity"])+" 字段=值 ...")
			case "list", "table", "chart":
				rows := app.It.Store.All(show(el["entity"]))
				if len(rows) == 0 {
					out = append(out, "  （"+show(el["entity"])+" 暂无数据）")
				}
				for _, r := range rows {
					out = append(out, "  #"+show(r.M["id"])+" "+strings.Trim(show(r), "{}"))
				}
			}
		}
	}
	return out
}

// runTuiApp: interactive terminal client for an app (line-oriented, works everywhere incl. Windows consoles).
func runTuiApp(app *App) {
	in := bufio.NewScanner(os.Stdin)
	role := "user"
	for {
		fmt.Println("\x1b[2J\x1b[H" + strings.Join(renderAppText(app, 80), "\n"))
		fmt.Printf("\n角色=%s  命令: add 实体 k=v.. | do 动作 [id] | del 实体 id | flow 名 | role 名 | quit\n> ", role)
		if !in.Scan() {
			return
		}
		f := strings.Fields(in.Text())
		if len(f) == 0 {
			continue
		}
		var code int
		var body []byte
		switch f[0] {
		case "quit", "q", "exit":
			return
		case "role":
			if len(f) > 1 {
				role = f[1]
			}
			continue
		case "add":
			if len(f) < 2 {
				continue
			}
			rec := NewRec()
			for _, kv := range f[2:] {
				k, v, _ := strings.Cut(kv, "=")
				if n, ok := parseNum(v); ok && !strings.Contains(v, ".") {
					rec.Set(k, int64(n))
				} else if n, ok := parseNum(v); ok {
					rec.Set(k, n)
				} else if v == "true" || v == "false" {
					rec.Set(k, v == "true")
				} else {
					rec.Set(k, v)
				}
			}
			code, _, body = app.Handle("POST", "/api/"+f[1], []byte(toJSON(rec)), role)
		case "do":
			if len(f) < 2 {
				continue
			}
			payload := "{}"
			if len(f) > 2 {
				av := &AppView{App: app}
				payload = `{"` + av.actionParam(app.Spec(), f[1]) + `": ` + f[2] + `}`
			}
			code, _, body = app.Handle("POST", "/api/action/"+f[1], []byte(payload), role)
		case "del":
			if len(f) < 3 {
				continue
			}
			code, _, body = app.Handle("DELETE", "/api/"+f[1]+"/"+f[2], nil, role)
		case "flow":
			if len(f) < 2 {
				continue
			}
			code, _, body = app.Handle("POST", "/api/flow/"+f[1], nil, role)
		default:
			continue
		}
		if code >= 400 || f[0] == "flow" {
			fmt.Printf("[%d] %s\n(回车继续)", code, body)
			in.Scan()
		}
	}
}

var _ = utf8.RuneLen
