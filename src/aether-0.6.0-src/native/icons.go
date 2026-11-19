package main

// Font icons: VS Code Codicons (CC-BY-4.0, assets/CODICONS-LICENSE.txt) embedded and rendered by the native canvas.
// No emoji are used anywhere in the UI; every icon is a glyph from this icon font.

import (
	_ "embed"
	"image"
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed assets/codicon.ttf
var fontIcon []byte

var (
	iconOnce  sync.Once
	otIcon    *opentype.Font
	iconFaces = map[int]font.Face{}
	iconMu    sync.Mutex
)

func iconFace(size int) font.Face {
	iconOnce.Do(func() { otIcon, _ = opentype.Parse(fontIcon) })
	iconMu.Lock()
	defer iconMu.Unlock()
	if f, ok := iconFaces[size]; ok {
		return f
	}
	f, _ := opentype.NewFace(otIcon, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingNone})
	iconFaces[size] = f
	return f
}

// Icon draws codicon `name` in a size x size box at (x,y); returns the advance (size).
func (c *Canvas) Icon(x, y int, name string, size int, col color.RGBA, clip image.Rectangle) int {
	r, ok := codiconMap[name]
	if !ok {
		return size
	}
	dst, ok2 := c.Img.SubImage(clip.Intersect(c.Img.Rect)).(*image.RGBA)
	if !ok2 || dst.Rect.Empty() {
		return size
	}
	f := iconFace(size)
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(col), Face: f}
	adv := d.MeasureString(string(r))
	m := f.Metrics()
	gh := m.Ascent + m.Descent
	d.Dot = fixed.Point26_6{X: fixed.I(x) + (fixed.I(size)-adv)/2, Y: fixed.I(y) + (fixed.I(size)-gh)/2 + m.Ascent}
	d.DrawString(string(r))
	return size
}

// IconText draws an icon followed by a label; returns the total width.
func (c *Canvas) IconText(x, y int, icon, s string, size int, mono bool, col color.RGBA, clip image.Rectangle) int {
	lh := LineH(size)
	isz := size + 2
	c.Icon(x, y+(lh-isz)/2, icon, isz, col, clip)
	return isz + 5 + c.Text(x+isz+5, y, s, size, mono, col, clip)
}
func MeasureIconText(s string, size int) int { return size + 2 + 5 + Measure(s, size, false) }
