package main

// Scripted UI automation for demos, screenshots and GUI smoke tests (AETHER_SCRIPT / --script).
//   ask:<prompt>; accept; tab:<diag|out|tests|ctr|exp|hist|ast>; run; test; click:x,y; type:<text>;
//   key:<enter|ctrl+z|...>; shot:<file.png>; sleep:<ms>; quit

import (
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"
)

func savePNG(c *Canvas, path string) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	png.Encode(f, c.Img)
	f.Close()
}

func runScript(g *GuiStudio, script string, redraw func()) {
	for _, cmd := range strings.Split(script, ";") {
		cmd = strings.TrimSpace(cmd)
		k, v, _ := strings.Cut(cmd, ":")
		switch k {
		case "ask":
			g.ask(v)
		case "accept":
			for i := len(g.S.Chat) - 1; i >= 0; i-- {
				if g.S.Chat[i].Prop != nil {
					g.S.Accept(g.S.Chat[i])
					g.Ed.Set(g.S.Src)
					g.lspChange()
					g.AV.App = g.S.Preview
					break
				}
			}
		case "tab": // bottom panel tab
			g.PanelOpen, g.PanelTab = true, v
		case "side": // activity: explorer|search|history|testing|run
			g.SideOpen, g.Activity = true, v
			if v == "testing" {
				g.S.RunTests()
			}
		case "cmd": // any command-palette command id
			g.exec(v)
		case "palette":
			g.commandPalette()
			g.Pal.Input = v
		case "goto": // line,col (1-based)
			a, b, _ := strings.Cut(v, ",")
			l, _ := strconv.Atoi(a)
			c, _ := strconv.Atoi(b)
			g.Ed.SetLineCol(l-1, c-1)
			g.Focus = "editor"
		case "edit": // replace first occurrence: old=>new
			o, n, _ := strings.Cut(v, "=>")
			g.setSource(strings.Replace(g.Ed.String(), o, n, 1))
		case "run":
			g.exec("run")
		case "test":
			g.exec("test")
		case "type":
			for _, r := range v {
				g.Key(Event{Type: EvKey, Rune: r})
			}
			g.lastEdit = time.Now()
		case "key", "click", "shot", "sleep", "quit":
			scriptCommon(k, v, func(ev Event) {
				if ev.Type == EvKey {
					g.Key(ev)
				} else {
					g.Mouse(ev)
				}
			}, g.UI.C, func() {
				if g.Ed.Dirty {
					g.sync()
				}
				redraw()
			})
		}
		redraw()
	}
}
func scriptCommon(k, v string, send func(Event), c *Canvas, redraw func()) {
	switch k {
	case "key":
		ev := Event{Type: EvKey}
		for {
			if strings.HasPrefix(v, "ctrl+") {
				ev.Ctrl, v = true, v[5:]
			} else if strings.HasPrefix(v, "shift+") {
				ev.Shift, v = true, v[6:]
			} else {
				break
			}
		}
		named := map[string]int{"enter": KEnter, "down": KDown, "up": KUp, "left": KLeft, "right": KRight, "pgdn": KPgDn, "pgup": KPgUp, "backspace": KBackspace, "esc": KEsc, "tab": KTab, "home": KHome, "end": KEnd, "delete": KDelete}
		for i := 1; i <= 12; i++ {
			named["f"+strconv.Itoa(i)] = KF1 + i - 1
		}
		if k, ok := named[v]; ok {
			ev.Key = k
		} else if v == "space" {
			ev.Rune = ' '
		} else if r := []rune(v); len(r) == 1 {
			ev.Rune = r[0]
			if ev.Shift && r[0] >= 'a' && r[0] <= 'z' {
				ev.Rune = r[0] - 32
			}
		}
		send(ev)
	case "click":
		a, b, _ := strings.Cut(v, ",")
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		send(Event{Type: EvMouse, Button: 1, X: x, Y: y})
	case "shot":
		redraw()
		savePNG(c, v)
	case "sleep":
		n, _ := strconv.Atoi(v)
		time.Sleep(time.Duration(n) * time.Millisecond)
	case "quit":
		redraw()
		os.Exit(0)
	}
}

// runAppWindow: run an Aether app as a native desktop window (no browser).
func runAppWindow(app *App, win Window, script string) {
	u := &UI{Vals: map[string]string{}}
	av := &AppView{App: app}
	w, h := win.Size()
	u.C = NewCanvas(w, h)
	redraw := func() {
		ww, hh := win.Size()
		if u.C.Img.Rect.Dx() != ww || u.C.Img.Rect.Dy() != hh {
			u.C = NewCanvas(ww, hh)
		}
		u.Hits = u.Hits[:0]
		av.Render(u, u.C.Img.Rect)
		win.Present(u.C.Img)
	}
	handle := func(ev Event) {
		switch ev.Type {
		case EvKey:
			if u.Focus != "" {
				v := u.Vals[u.Focus]
				if ev.Key == KBackspace {
					if r := []rune(v); len(r) > 0 {
						u.Vals[u.Focus] = string(r[:len(r)-1])
					}
				} else if ev.Rune != 0 {
					u.Vals[u.Focus] = v + string(ev.Rune)
				}
			}
			if ev.Key == KPgDn || ev.Key == KDown {
				av.Scroll += 60
			}
			if ev.Key == KPgUp || ev.Key == KUp {
				av.Scroll = max(0, av.Scroll-60)
			}
		case EvMouse:
			if ev.Button == 4 {
				av.Scroll = max(0, av.Scroll-60)
			} else if ev.Button == 5 {
				av.Scroll += 60
			} else {
				u.Click(ev.X, ev.Y)
			}
		}
	}
	redraw()
	for _, cmd := range strings.Split(script, ";") {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		k, v, _ := strings.Cut(cmd, ":")
		if k == "type" {
			for _, r := range v {
				handle(Event{Type: EvKey, Rune: r})
			}
		} else {
			scriptCommon(k, v, handle, u.C, redraw)
		}
		redraw()
	}
	for ev := range win.Events() {
		if ev.Type == EvClose {
			return
		}
		handle(ev)
		redraw()
	}
}
