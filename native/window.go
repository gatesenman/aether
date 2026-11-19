package main

import "image"

type EvType int

const (
	EvNone EvType = iota
	EvKey
	EvMouse
	EvExpose
	EvResize
	EvClose
	EvMove
)
const (
	KNone = iota
	KBackspace
	KEnter
	KTab
	KEsc
	KLeft
	KRight
	KUp
	KDown
	KDelete
	KHome
	KEnd
	KPgUp
	KPgDn
	KF1
	KF2
	KF3
	KF4
	KF5
	KF6
	KF7
	KF8
	KF9
	KF10
	KF11
	KF12
)

type Event struct {
	Type        EvType
	Key         int
	Rune        rune
	Ctrl, Shift bool
	Button      int
	X, Y        int
}
type Window interface {
	Present(img *image.RGBA)
	Events() chan Event
	Size() (int, int)
}

// Clipboard is implemented by windows that can talk to the system clipboard (X11 CLIPBOARD selection).
type Clipboard interface {
	SetClipboard(s string)
	GetClipboard() string
}

// memClip: fallback clipboard (offscreen / no system clipboard).
var memClip string
