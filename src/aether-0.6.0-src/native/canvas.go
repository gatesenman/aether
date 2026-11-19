package main

// Own 2D canvas: framebuffer, rectangles, and text rendering with embedded fonts
// (glyph outlines rasterised by golang.org/x/image's sfnt/opentype rasteriser — the one third-party library used).

import (
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed assets/aether-ui.otf
var fontUI []byte

//go:embed assets/aether-mono.otf
var fontMono []byte

var (
	fontOnce     sync.Once
	otUI, otMono *opentype.Font
	faceCache    = map[[2]int]font.Face{}
	faceMu       sync.Mutex
)

func face(mono bool, size int) font.Face {
	fontOnce.Do(func() {
		otUI, _ = opentype.Parse(fontUI)
		otMono, _ = opentype.Parse(fontMono)
	})
	k := [2]int{b2int(mono), size}
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[k]; ok {
		return f
	}
	src := otUI
	if mono {
		src = otMono
	}
	f, _ := opentype.NewFace(src, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
	faceCache[k] = f
	return f
}
func b2int(b bool) int {
	if b {
		return 1
	}
	return 0
}

type Canvas struct{ Img *image.RGBA }

func NewCanvas(w, h int) *Canvas { return &Canvas{image.NewRGBA(image.Rect(0, 0, w, h))} }
func rgb(hex uint32) color.RGBA {
	return color.RGBA{uint8(hex >> 16), uint8(hex >> 8), uint8(hex), 255}
}
func (c *Canvas) Fill(r image.Rectangle, col color.RGBA) {
	draw.Draw(c.Img, r.Intersect(c.Img.Rect), image.NewUniform(col), image.Point{}, draw.Src)
}
func (c *Canvas) Border(r image.Rectangle, col color.RGBA) {
	c.Fill(image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1), col)
	c.Fill(image.Rect(r.Min.X, r.Max.Y-1, r.Max.X, r.Max.Y), col)
	c.Fill(image.Rect(r.Min.X, r.Min.Y, r.Min.X+1, r.Max.Y), col)
	c.Fill(image.Rect(r.Max.X-1, r.Min.Y, r.Max.X, r.Max.Y), col)
}

// RoundRect: filled rectangle with small rounded corners
func (c *Canvas) RoundRect(r image.Rectangle, rad int, col color.RGBA) {
	if r.Dx() < 2*rad || r.Dy() < 2*rad {
		c.Fill(r, col)
		return
	}
	c.Fill(image.Rect(r.Min.X+rad, r.Min.Y, r.Max.X-rad, r.Max.Y), col)
	c.Fill(image.Rect(r.Min.X, r.Min.Y+rad, r.Max.X, r.Max.Y-rad), col)
	for dy := 0; dy < rad; dy++ {
		for dx := 0; dx < rad; dx++ {
			if (rad-dx)*(rad-dx)+(rad-dy)*(rad-dy) <= rad*rad+rad {
				for _, p := range []image.Point{{r.Min.X + dx, r.Min.Y + dy}, {r.Max.X - 1 - dx, r.Min.Y + dy}, {r.Min.X + dx, r.Max.Y - 1 - dy}, {r.Max.X - 1 - dx, r.Max.Y - 1 - dy}} {
					if p.In(c.Img.Rect) {
						c.Img.SetRGBA(p.X, p.Y, col)
					}
				}
			}
		}
	}
}

// Text draws s with its top-left at (x,y) clipped to clip; returns advance width.
func (c *Canvas) Text(x, y int, s string, size int, mono bool, col color.RGBA, clip image.Rectangle) int {
	f := face(mono, size)
	dst, ok := c.Img.SubImage(clip.Intersect(c.Img.Rect)).(*image.RGBA)
	if !ok || dst.Rect.Empty() {
		return Measure(s, size, mono)
	}
	m := f.Metrics()
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(col), Face: f, Dot: fixed.P(x, y+m.Ascent.Ceil())}
	d.DrawString(s)
	return (d.Dot.X - fixed.I(x)).Ceil()
}
func Measure(s string, size int, mono bool) int {
	return font.MeasureString(face(mono, size), s).Ceil()
}
func LineH(size int) int { m := face(false, size).Metrics(); return (m.Ascent + m.Descent).Ceil() }

// Palette
var (
	cBg      = rgb(0xf5f6fa)
	cCard    = rgb(0xffffff)
	cInk     = rgb(0x1d2433)
	cMuted   = rgb(0x778099)
	cAccent  = rgb(0x4f46e5)
	cAccent2 = rgb(0x7c3aed)
	cLine    = rgb(0xe3e6f0)
	cGhost   = rgb(0xeef0ff)
	cDanger  = rgb(0xb91c1c)
	cDangerB = rgb(0xfee2e2)
	cWhite   = rgb(0xffffff)
	cOK      = rgb(0x16a34a)
	// dark (studio)
	dBg    = rgb(0x0f1222)
	dPane  = rgb(0x12152a)
	dBar   = rgb(0x171a30)
	dLine  = rgb(0x262a48)
	dInk   = rgb(0xdfe3f5)
	dMuted = rgb(0x7f86b4)
	dSel   = rgb(0x2a2f55)
	dErr   = rgb(0xf87171)
	dWarn  = rgb(0xfbbf24)
	dOK    = rgb(0x4ade80)
	dPurp  = rgb(0xa5b4fc)
)
