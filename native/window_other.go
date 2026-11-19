//go:build !(linux || freebsd || openbsd || netbsd)

package main

import "errors"

func openWindow(title string, w, h int) (Window, error) {
	return nil, errors.New("native window backend not yet implemented on this OS; falling back to terminal UI")
}
