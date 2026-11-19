// Aether Setup SFX: extracts an appended ZIP payload and runs Install-Aether.ps1.
// Layout: [sfx.exe bytes][magic 16 bytes][uint64 zipSize LE][zip bytes]
package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var magic = []byte("AETHERSFX0.6.0!!") // 16 bytes

func main() {
	exe, err := os.Executable()
	if err != nil {
		fail(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		fail(err)
	}
	idx := bytes.LastIndex(data, magic)
	if idx < 0 || idx+16+8 > len(data) {
		fail(fmt.Errorf("这个 Setup 没有附带安装包数据（magic 未找到）"))
	}
	zipSize := binary.LittleEndian.Uint64(data[idx+16 : idx+24])
	start := idx + 24
	end := start + int(zipSize)
	if end > len(data) {
		fail(fmt.Errorf("安装包数据不完整: want %d have %d", zipSize, len(data)-start))
	}
	zipBytes := data[start:end]

	tmp, err := os.MkdirTemp("", "aether-setup-*")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(tmp)

	fmt.Println("Aether Setup 0.6.0 — 正在解压…")
	if err := unzipBytes(zipBytes, tmp); err != nil {
		fail(err)
	}
	ps1 := findInstallScript(tmp)
	if ps1 == "" {
		fail(fmt.Errorf("压缩包里没有 Install-Aether.ps1"))
	}
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ps1)
	cmd.Dir = filepath.Dir(ps1)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fail(err)
	}
}

func findInstallScript(root string) string {
	var found string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(info.Name(), "Install-Aether.ps1") {
			found = p
			return io.EOF
		}
		return nil
	})
	return found
}

func unzipBytes(b []byte, dest string) error {
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return err
	}
	for _, f := range r.File {
		name := filepath.FromSlash(f.Name)
		out := filepath.Join(dest, name)
		cleanDest := filepath.Clean(dest)
		cleanOut := filepath.Clean(out)
		if cleanOut != cleanDest && !strings.HasPrefix(cleanOut, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("非法路径: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(out, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(out), 0755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(w, rc)
		w.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "Aether Setup 失败:", err)
	fmt.Fprintln(os.Stderr, "按回车退出…")
	fmt.Scanln()
	os.Exit(1)
}
