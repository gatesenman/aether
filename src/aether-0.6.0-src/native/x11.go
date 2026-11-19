//go:build linux || freebsd || openbsd || netbsd

package main

// Minimal X11 wire-protocol client written from scratch (no Xlib, no cgo):
// connect + MIT-MAGIC-COOKIE auth, create window, PutImage of our own framebuffer, keyboard/mouse events.

import (
	"encoding/binary"
	"errors"
	"image"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type xconn struct {
	readerOn bool
	keyQ     chan func()
	clip                          string
	clipMu                        sync.Mutex
	selCh                         chan string
	aClip, aUTF8, aTargets, aProp uint32
	c                             net.Conn
	mu                            sync.Mutex
	idBase, idMask, next          uint32
	root, wid, gc                 uint32
	depth                         byte
	maxReq                        int
	minKC, maxKC                  byte
	keysyms                       []uint32
	perKC                         int
	events                        chan Event
	W, H                          int
	wmDelete, wmProtocols         uint32
	replies                       chan []byte
}

var le = binary.LittleEndian

func pad4(n int) int { return (4 - n%4) % 4 }

func readXauth(dispNum string) (string, []byte) {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		path = filepath.Join(os.Getenv("HOME"), ".Xauthority")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	be := binary.BigEndian
	rd := func(p *int) []byte {
		if *p+2 > len(data) {
			return nil
		}
		n := int(be.Uint16(data[*p:]))
		*p += 2
		if *p+n > len(data) {
			return nil
		}
		b := data[*p : *p+n]
		*p += n
		return b
	}
	p := 0
	for p+2 < len(data) {
		p += 2 // family
		rd(&p)
		num := string(rd(&p))
		name := string(rd(&p))
		d := rd(&p)
		if (num == dispNum || num == "") && name == "MIT-MAGIC-COOKIE-1" {
			return name, d
		}
	}
	return "", nil
}

func openX(title string, w, h int) (*xconn, error) {
	disp := os.Getenv("DISPLAY")
	if disp == "" {
		return nil, errors.New("DISPLAY not set (no X server); use --tui or run under Xvfb")
	}
	i := strings.LastIndex(disp, ":")
	num := disp[i+1:]
	if j := strings.Index(num, "."); j >= 0 {
		num = num[:j]
	}
	var c net.Conn
	var err error
	if host := disp[:i]; host == "" || host == "unix" {
		c, err = net.Dial("unix", "/tmp/.X11-unix/X"+num)
	} else {
		n, _ := strconv.Atoi(num)
		c, err = net.Dial("tcp", host+":"+strconv.Itoa(6000+n))
	}
	if err != nil {
		return nil, err
	}
	an, ad := readXauth(num)
	req := make([]byte, 12)
	req[0] = 'l'
	le.PutUint16(req[2:], 11)
	le.PutUint16(req[6:], uint16(len(an)))
	le.PutUint16(req[8:], uint16(len(ad)))
	req = append(req, []byte(an)...)
	req = append(req, make([]byte, pad4(len(an)))...)
	req = append(req, ad...)
	req = append(req, make([]byte, pad4(len(ad)))...)
	c.Write(req)
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return nil, err
	}
	extra := make([]byte, int(le.Uint16(hdr[6:]))*4)
	io.ReadFull(c, extra)
	if hdr[0] != 1 {
		return nil, errors.New("X11 connection refused: " + string(extra[:min(len(extra), int(hdr[1]))]))
	}
	x := &xconn{c: c, events: make(chan Event, 256), W: w, H: h, replies: make(chan []byte, 4), selCh: make(chan string, 1)}
	x.idBase = le.Uint32(extra[4:])
	x.idMask = le.Uint32(extra[8:])
	vlen := int(le.Uint16(extra[16:]))
	x.maxReq = int(le.Uint16(extra[18:]))
	nscreens := extra[20]
	nformats := int(extra[21])
	x.minKC, x.maxKC = extra[26], extra[27]
	_ = nscreens
	off := 32 + vlen + pad4(vlen) + 8*nformats
	scr := extra[off:]
	x.root = le.Uint32(scr[0:])
	x.depth = scr[38]
	x.wid = x.newID()
	x.gc = x.newID()
	// CreateWindow
	vals := []uint32{0xffffff, 0x8000 | 0x1 | 0x4 | 0x20000 | 0x2 | 0x40}
	r := x.reqHdr(1, x.depth, 8+len(vals))
	r = put32(r, x.wid, x.root)
	r = put16(r, 0, 0, uint16(w), uint16(h), 0, 1)
	r = put32(r, 0, 0x2|0x800)
	r = put32(r, vals...)
	x.send(r)
	x.setTitle(title)
	// WM_DELETE_WINDOW
	x.wmProtocols = x.internAtom("WM_PROTOCOLS")
	x.wmDelete = x.internAtom("WM_DELETE_WINDOW")
	r = x.reqHdr(18, 0, 0)
	r = put32(r, x.wid, x.wmProtocols, 4)
	r = append(r, 32, 0, 0, 0)
	r = put32(r, 1, x.wmDelete)
	x.fixLen(r)
	x.send(r)
	// MapWindow, CreateGC
	x.send(put32(x.reqHdr(8, 0, 2), x.wid))
	x.send(put32(x.reqHdr(55, 0, 4), x.gc, x.wid, 0))
	x.atoms()
	x.readerOn = true
	x.keyQ = make(chan func(), 256)
	go func() { // key translation worker: a reload finishes before later keys are translated
		for f := range x.keyQ {
			f()
		}
	}()
	go x.readLoop()
	x.loadKeymap()
	return x, nil
}
func (x *xconn) newID() uint32 { id := x.idBase | (x.next & x.idMask); x.next++; return id }
func (x *xconn) reqHdr(op, data byte, words int) []byte {
	b := make([]byte, 4, max(4, words*4))
	b[0], b[1] = op, data
	le.PutUint16(b[2:], uint16(words))
	return b
}
func (x *xconn) fixLen(b []byte) { le.PutUint16(b[2:], uint16(len(b)/4)) }
func put32(b []byte, v ...uint32) []byte {
	for _, x := range v {
		b = le.AppendUint32(b, x)
	}
	return b
}
func put16(b []byte, v ...uint16) []byte {
	for _, x := range v {
		b = le.AppendUint16(b, x)
	}
	return b
}
func (x *xconn) send(b []byte) { x.mu.Lock(); x.c.Write(b); x.mu.Unlock() }
func (x *xconn) internAtom(name string) uint32 {
	r := x.reqHdr(16, 0, 0)
	r = put16(r, uint16(len(name)), 0)
	r = append(r, name...)
	r = append(r, make([]byte, pad4(len(name)))...)
	x.fixLen(r)
	x.send(r)
	rep := x.readReplyDirect()
	return le.Uint32(rep[8:])
}


func (x *xconn) readReplyDirect() []byte {
	if x.readerOn {
		return <-x.replies
	}
	for {
		p := make([]byte, 32)
		io.ReadFull(x.c, p)
		if p[0] == 1 {
			n := int(le.Uint32(p[4:])) * 4
			more := make([]byte, n)
			io.ReadFull(x.c, more)
			return append(p, more...)
		}
		x.dispatch(p)
	}
}
func (x *xconn) setTitle(t string) {
	for _, prop := range []string{"WM_NAME", "_NET_WM_NAME"} {
		var atom, typ uint32 = 39, 31
		if prop == "_NET_WM_NAME" {
			atom = x.internAtom("_NET_WM_NAME")
			typ = x.internAtom("UTF8_STRING")
		}
		r := x.reqHdr(18, 0, 0)
		r = put32(r, x.wid, atom, typ)
		r = append(r, 8, 0, 0, 0)
		r = put32(r, uint32(len(t)))
		r = append(r, t...)
		r = append(r, make([]byte, pad4(len(t)))...)
		x.fixLen(r)
		x.send(r)
	}
}
func (x *xconn) loadKeymap() {
	cnt := int(x.maxKC) - int(x.minKC) + 1
	r := x.reqHdr(101, 0, 2)
	r = append(r, x.minKC, byte(cnt), 0, 0)
	x.send(r)
	rep := <-x.replies
	x.perKC = int(rep[1])
	n := int(le.Uint32(rep[4:]))
	ks := make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		ks = append(ks, le.Uint32(rep[32+4*i:]))
	}
	x.keysyms = ks
}
func (x *xconn) readLoop() {
	for {
		p := make([]byte, 32)
		if _, err := io.ReadFull(x.c, p); err != nil {
			x.events <- Event{Type: EvClose}
			return
		}
		if p[0] == 1 {
			n := int(le.Uint32(p[4:])) * 4
			more := make([]byte, n)
			io.ReadFull(x.c, more)
			x.replies <- append(p, more...)
			continue
		}
		x.dispatch(p)
	}
}
func (x *xconn) dispatch(p []byte) {
	switch p[0] & 0x7f {
	case 2: // KeyPress
		kc := p[1]
		state := le.Uint16(p[28:])
		x.keyQ <- func() { x.events <- x.keyEvent(kc, state) } // ordered with keymap reloads
	case 4: // ButtonPress
		x.events <- Event{Type: EvMouse, Button: int(p[1]), X: int(int16(le.Uint16(p[24:]))), Y: int(int16(le.Uint16(p[26:])))}
	case 6: // MotionNotify (coalesced: dropped when the queue is busy)
		select {
		case x.events <- Event{Type: EvMove, X: int(int16(le.Uint16(p[24:]))), Y: int(int16(le.Uint16(p[26:])))}:
		default:
		}
	case 30: // SelectionRequest: another client wants our clipboard
		go x.answerSelection(le.Uint32(p[8:]), le.Uint32(p[12:]), le.Uint32(p[16:]), le.Uint32(p[20:]), le.Uint32(p[24:]))
	case 31: // SelectionNotify: our paste request was answered
		prop := le.Uint32(p[20:])
		go func() {
			if prop == 0 {
				x.selCh <- ""
				return
			}
			x.selCh <- x.getProp(prop)
		}()
	case 34: // MappingNotify: keyboard map changed (e.g. xdotool typing CJK, layout switch)
		if p[4] == 1 { // request: 0 modifier, 1 keyboard, 2 pointer
			x.keyQ <- x.loadKeymap
		}
	case 12:
		x.events <- Event{Type: EvExpose}
	case 22: // ConfigureNotify
		w, h := int(le.Uint16(p[20:])), int(le.Uint16(p[22:]))
		if w != x.W || h != x.H {
			x.W, x.H = w, h
			x.events <- Event{Type: EvResize, X: w, Y: h}
		}
	case 33: // ClientMessage
		if le.Uint32(p[12:]) == x.wmDelete {
			x.events <- Event{Type: EvClose}
		}
	}
}
func (x *xconn) keyEvent(kc byte, state uint16) Event {
	idx := (int(kc) - int(x.minKC)) * x.perKC
	if idx < 0 || idx >= len(x.keysyms) {
		return Event{Type: EvNone}
	}
	shift := state&1 != 0
	caps := state&2 != 0
	ks := x.keysyms[idx]
	if (shift || caps) && x.perKC > 1 && x.keysyms[idx+1] != 0 {
		alt := x.keysyms[idx+1]
		if shift || (caps && alt >= 'A' && alt <= 'Z') {
			ks = alt
		}
	}
	ev := Event{Type: EvKey, Ctrl: state&4 != 0, Shift: shift}
	switch ks {
	case 0xff08:
		ev.Key = KBackspace
	case 0xff0d, 0xff8d:
		ev.Key = KEnter
	case 0xff09:
		ev.Key = KTab
	case 0xff1b:
		ev.Key = KEsc
	case 0xff51:
		ev.Key = KLeft
	case 0xff52:
		ev.Key = KUp
	case 0xff53:
		ev.Key = KRight
	case 0xff54:
		ev.Key = KDown
	case 0xffff:
		ev.Key = KDelete
	case 0xff50:
		ev.Key = KHome
	case 0xff57:
		ev.Key = KEnd
	case 0xff55:
		ev.Key = KPgUp
	case 0xff56:
		ev.Key = KPgDn
	default:
		if ks >= 0xffbe && ks <= 0xffc9 {
			ev.Key = KF1 + int(ks-0xffbe)
		} else if ks >= 0x20 && ks <= 0xff {
			ev.Rune = rune(ks)
		} else if ks&0xff000000 == 0x01000000 {
			ev.Rune = rune(ks & 0xffffff)
		} else {
			ev.Type = EvNone
		}
	}
	return ev
}

// Present: send our RGBA framebuffer with PutImage (ZPixmap, 32bpp BGRX), split to fit max request size.
func (x *xconn) Present(img *image.RGBA) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	maxBytes := x.maxReq*4 - 24
	rows := maxBytes / (w * 4)
	if rows < 1 {
		rows = 1
	}
	buf := make([]byte, 0, 24+rows*w*4)
	for y0 := 0; y0 < h; y0 += rows {
		n := min(rows, h-y0)
		buf = buf[:0]
		buf = append(buf, 72, 2, 0, 0)
		buf = put32(buf, x.wid, x.gc)
		buf = put16(buf, uint16(w), uint16(n), 0, uint16(y0))
		buf = append(buf, 0, 24, 0, 0)
		for y := y0; y < y0+n; y++ {
			row := img.Pix[y*img.Stride : y*img.Stride+w*4]
			for i := 0; i < len(row); i += 4 {
				buf = append(buf, row[i+2], row[i+1], row[i], 0)
			}
		}
		le.PutUint16(buf[2:], uint16(len(buf)/4))
		x.send(buf)
	}
}
func (x *xconn) Events() chan Event { return x.events }
func (x *xconn) Size() (int, int)   { return x.W, x.H }

func openWindow(title string, w, h int) (Window, error) { return openX(title, w, h) }

// ---- clipboard (ICCCM CLIPBOARD selection, UTF8_STRING) ----
func (x *xconn) atoms() {
	if x.aClip != 0 {
		return
	}
	x.aClip = x.internAtom("CLIPBOARD")
	x.aUTF8 = x.internAtom("UTF8_STRING")
	x.aTargets = x.internAtom("TARGETS")
	x.aProp = x.internAtom("AETHER_SEL")
}
func (x *xconn) SetClipboard(s string) {
	memClip = s
	x.clipMu.Lock()
	x.clip = s
	x.clipMu.Unlock()
	x.atoms()
	r := x.reqHdr(22, 0, 4) // SetSelectionOwner
	r = put32(r, x.wid, x.aClip, 0)
	x.send(r)
}
func (x *xconn) GetClipboard() string {
	x.atoms()
	r := x.reqHdr(24, 0, 6) // ConvertSelection
	r = put32(r, x.wid, x.aClip, x.aUTF8, x.aProp, 0)
	x.send(r)
	select {
	case s := <-x.selCh:
		return s
	case <-time.After(700 * time.Millisecond):
		return memClip
	}
}
func (x *xconn) getProp(prop uint32) string {
	r := x.reqHdr(20, 1, 6) // GetProperty, delete=1
	r = put32(r, x.wid, prop, 0, 0, 1<<20)
	x.send(r)
	rep := <-x.replies
	n := int(le.Uint32(rep[16:]))
	if 32+n > len(rep) {
		return ""
	}
	return string(rep[32 : 32+n])
}
func (x *xconn) answerSelection(tm, requestor, selection, target, property uint32) {
	x.atoms()
	if property == 0 {
		property = target
	}
	x.clipMu.Lock()
	data := x.clip
	x.clipMu.Unlock()
	ok := true
	switch target {
	case x.aTargets:
		r := x.reqHdr(18, 0, 0)
		r = put32(r, requestor, property, 4) // type ATOM
		r = append(r, 32, 0, 0, 0)
		r = put32(r, 2, x.aTargets, x.aUTF8)
		x.fixLen(r)
		x.send(r)
	case x.aUTF8, 31 /* STRING */ :
		r := x.reqHdr(18, 0, 0)
		r = put32(r, requestor, property, target)
		r = append(r, 8, 0, 0, 0)
		r = put32(r, uint32(len(data)))
		r = append(r, data...)
		r = append(r, make([]byte, pad4(len(data)))...)
		x.fixLen(r)
		x.send(r)
	default:
		ok = false
	}
	if !ok {
		property = 0
	}
	ev := make([]byte, 32) // SelectionNotify sent via SendEvent
	ev[0] = 31
	le.PutUint32(ev[4:], tm)
	le.PutUint32(ev[8:], requestor)
	le.PutUint32(ev[12:], selection)
	le.PutUint32(ev[16:], target)
	le.PutUint32(ev[20:], property)
	r := x.reqHdr(25, 0, 11)
	r = put32(r, requestor, 0)
	r = append(r, ev...)
	x.send(r)
}
