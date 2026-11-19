package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	ide := filepath.Join(base, "Aether", "IDE", "Aether IDE.exe")
	if _, err := os.Stat(ide); err != nil {
		// portable: sibling IDE
		if self, err2 := os.Executable(); err2 == nil {
			alt := filepath.Join(filepath.Dir(self), "IDE", "Aether IDE.exe")
			if _, err3 := os.Stat(alt); err3 == nil {
				ide = alt
			}
		}
	}
	if _, err := os.Stat(ide); err != nil {
		fmt.Fprintln(os.Stderr, "找不到 Aether IDE。请先运行 Aether-Setup 或 Install-Aether.ps1")
		fmt.Fprintln(os.Stderr, "期望路径:", ide)
		os.Exit(1)
	}
	cmd := exec.Command(ide, os.Args[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
