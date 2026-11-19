//go:build linux

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Two independent X11 connections: A owns CLIPBOARD, B converts it (real ICCCM round-trip through the X server).
func TestX11ClipboardCrossClient(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no DISPLAY")
	}
	a, err := openX("clip-a", 100, 100)
	if err != nil {
		t.Skip(err)
	}
	b, err := openX("clip-b", 100, 100)
	if err != nil {
		t.Skip(err)
	}
	a.SetClipboard("你好 Aether 剪贴板")
	memClip = "" // make sure B cannot cheat via the in-process fallback
	if got := b.GetClipboard(); got != "你好 Aether 剪贴板" {
		t.Fatalf("got %q", got)
	}
	b.SetClipboard("from b")
	time.Sleep(150 * time.Millisecond) // two connections: let the server process the ownership change first
	memClip = ""
	if got := a.GetClipboard(); got != "from b" {
		t.Fatalf("got %q", got)
	}
}

// Used by the live Studio check: AE_CLIP_EXPECT=<text> go test -run X11ClipboardRead
func TestX11ClipboardRead(t *testing.T) {
	want := os.Getenv("AE_CLIP_EXPECT")
	if want == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("live check only")
	}
	c, err := openX("clip-reader", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	memClip = ""
	got := c.GetClipboard()
	if !strings.Contains(got, want) {
		t.Fatalf("clipboard = %q, want it to contain %q", got, want)
	}
	if w := os.Getenv("AE_CLIP_SET"); w != "" {
		c.SetClipboard(w)
		time.Sleep(4 * time.Second) // stay owner while the Studio pastes
	}
}
