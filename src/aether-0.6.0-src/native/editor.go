package main

import "strings"

// Editor: rune-buffer text editor model shared by the window and terminal Studios.
type Editor struct {
	Buf    []rune
	Cur    int
	Top    int // first visible line
	Dirty  bool
	Anchor int // selection anchor (-1 = none)
}

func (e *Editor) SelRange() (int, int, bool) {
	if e.Anchor < 0 || e.Anchor == e.Cur || e.Anchor > len(e.Buf) {
		return 0, 0, false
	}
	if e.Anchor < e.Cur {
		return e.Anchor, e.Cur, true
	}
	return e.Cur, e.Anchor, true
}
func (e *Editor) Selected() string {
	if a, b, ok := e.SelRange(); ok {
		return string(e.Buf[a:b])
	}
	return ""
}
func (e *Editor) DeleteSel() bool {
	a, b, ok := e.SelRange()
	if !ok {
		e.Anchor = -1
		return false
	}
	e.Buf = append(e.Buf[:a], e.Buf[b:]...)
	e.Cur, e.Anchor, e.Dirty = a, -1, true
	return true
}

// LineBounds returns [start,end) of the caret line including its newline.
func (e *Editor) LineBounds() (int, int) {
	a := e.Cur
	for a > 0 && e.Buf[a-1] != '\n' {
		a--
	}
	b := e.Cur
	for b < len(e.Buf) && e.Buf[b] != '\n' {
		b++
	}
	if b < len(e.Buf) {
		b++
	}
	return a, b
}

func (e *Editor) Set(s string) {
	e.Buf = []rune(s)
	e.Anchor = -1
	if e.Cur > len(e.Buf) {
		e.Cur = len(e.Buf)
	}
}
func (e *Editor) String() string  { return string(e.Buf) }
func (e *Editor) Lines() []string { return strings.Split(string(e.Buf), "\n") }
func (e *Editor) LineCol() (int, int) {
	l, c := 0, 0
	for i := 0; i < e.Cur && i < len(e.Buf); i++ {
		if e.Buf[i] == '\n' {
			l++
			c = 0
		} else {
			c++
		}
	}
	return l, c
}
func (e *Editor) SetLineCol(l, c int) {
	lines := e.Lines()
	if l < 0 {
		l = 0
	}
	if l >= len(lines) {
		l = len(lines) - 1
	}
	off := 0
	for i := 0; i < l; i++ {
		off += len([]rune(lines[i])) + 1
	}
	lr := len([]rune(lines[l]))
	if c > lr {
		c = lr
	}
	if c < 0 {
		c = 0
	}
	e.Cur = off + c
}
func (e *Editor) Insert(s string) {
	e.DeleteSel()
	r := []rune(s)
	e.Buf = append(e.Buf[:e.Cur], append(r, e.Buf[e.Cur:]...)...)
	e.Cur += len(r)
	e.Dirty = true
}
func (e *Editor) Key(ev Event) bool {
	l, c := e.LineCol()
	nav := ev.Key == KLeft || ev.Key == KRight || ev.Key == KUp || ev.Key == KDown || ev.Key == KHome || ev.Key == KEnd || ev.Key == KPgUp || ev.Key == KPgDn
	if nav {
		if ev.Shift {
			if e.Anchor < 0 {
				e.Anchor = e.Cur
			}
		} else {
			e.Anchor = -1
		}
	}
	if (ev.Key == KBackspace || ev.Key == KDelete) && e.DeleteSel() {
		return true
	}
	switch ev.Key {
	case KBackspace:
		if e.Cur > 0 {
			e.Buf = append(e.Buf[:e.Cur-1], e.Buf[e.Cur:]...)
			e.Cur--
			e.Dirty = true
		}
	case KDelete:
		if e.Cur < len(e.Buf) {
			e.Buf = append(e.Buf[:e.Cur], e.Buf[e.Cur+1:]...)
			e.Dirty = true
		}
	case KEnter:
		// keep indentation
		line := e.Lines()[l]
		ind := len(line) - len(strings.TrimLeft(line, " "))
		extra := ""
		if strings.HasSuffix(strings.TrimSpace(line), "{") {
			extra = "  "
		}
		e.Insert("\n" + strings.Repeat(" ", ind) + extra)
	case KTab:
		e.Insert("  ")
	case KLeft:
		if e.Cur > 0 {
			e.Cur--
		}
	case KRight:
		if e.Cur < len(e.Buf) {
			e.Cur++
		}
	case KUp:
		e.SetLineCol(l-1, c)
	case KDown:
		e.SetLineCol(l+1, c)
	case KHome:
		e.SetLineCol(l, 0)
	case KEnd:
		e.SetLineCol(l, 1<<30)
	case KPgUp:
		e.SetLineCol(l-20, c)
	case KPgDn:
		e.SetLineCol(l+20, c)
	default:
		if ev.Rune != 0 && !ev.Ctrl {
			e.Insert(string(ev.Rune))
			return true
		}
		return false
	}
	return true
}
func (e *Editor) Scroll(visible int) {
	l, _ := e.LineCol()
	if l < e.Top {
		e.Top = l
	}
	if l >= e.Top+visible {
		e.Top = l - visible + 1
	}
	if e.Top < 0 {
		e.Top = 0
	}
}
