package main

// aether doctor — environment self-check for install / support.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func cmdDoctor() int {
	ok := true
	line := func(k, v string) { fmt.Printf("%-18s %s\n", k+":", v) }
	pass := func(k, v string) { line(k, "ok — "+v) }
	fail := func(k, v string) { ok = false; line(k, "FAIL — "+v) }

	line("aether", Version+" (native)")
	line("os", runtime.GOOS+"/"+runtime.GOARCH)
	line("go_runtime", runtime.Version())
	if exe, err := os.Executable(); err == nil {
		line("binary", exe)
	}
	line("cwd", mustAbs("."))
	line("path", os.Getenv("PATH"))

	switch runtime.GOOS {
	case "windows":
		line("display", "N/A (Windows — use Aether IDE / aether ui falls back to TUI without a console GUI)")
	case "linux":
		d := os.Getenv("DISPLAY")
		if d == "" {
			line("display", "unset (no X11; aether ui/studio use TUI)")
		} else {
			line("display", d+" (X11)")
		}
	case "darwin":
		line("display", "macOS (native GUI not yet; TUI / Aether IDE)")
	default:
		line("display", runtime.GOOS)
	}

	// Writable dirs
	cands := []string{}
	if td := os.TempDir(); td != "" {
		cands = append(cands, td)
	}
	if h, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(h, ".aether"))
		if runtime.GOOS == "windows" {
			cands = append(cands, filepath.Join(os.Getenv("LOCALAPPDATA"), "Aether"))
		}
	}
	for _, d := range cands {
		if err := os.MkdirAll(d, 0755); err != nil {
			fail("writable", d+": "+err.Error())
			continue
		}
		p := filepath.Join(d, ".aether-doctor-"+fmt.Sprint(time.Now().UnixNano()))
		if err := os.WriteFile(p, []byte("ok"), 0644); err != nil {
			fail("writable", d+": "+err.Error())
			continue
		}
		os.Remove(p)
		pass("writable", d)
		break
	}

	// Hello: parse + check a tiny program (CRLF + Chinese path soft-check)
	hello := "module 诊断\r\nentity Item { title: Text }\r\nfn ping() -> Int { 1 }\r\n"
	tmpRoot := os.TempDir()
	if runtime.GOOS == "windows" {
		tmpRoot = filepath.Join(os.Getenv("LOCALAPPDATA"), "Aether", "doctor")
		os.MkdirAll(tmpRoot, 0755)
	}
	dir := filepath.Join(tmpRoot, "你好Aether")
	os.MkdirAll(dir, 0755)
	ae := filepath.Join(dir, "hello.ae")
	if err := os.WriteFile(ae, []byte(hello), 0644); err != nil {
		fail("hello", "write: "+err.Error())
	} else {
		src, base, err := readSrc(ae)
		if err != nil {
			fail("hello", "read: "+err.Error())
		} else {
			_, ds := CheckSourceIn(src, base)
			errs := 0
			for _, d := range ds {
				if d["severity"] == "error" {
					errs++
				}
			}
			if errs > 0 {
				fail("hello", fmt.Sprintf("%d check errors on CRLF+CJK path", errs))
			} else {
				pass("hello", "check ok ("+ae+")")
			}
		}
		// AeStore round-trip
		db := filepath.Join(dir, "t.aedb")
		st, err := OpenStore(db)
		if err != nil {
			fail("store", err.Error())
		} else {
			st.Ensure(Node{"name": "Item", "fields": []any{Node{"name": "title", "type": Node{"name": "Text"}}}})
			r := NewRec(); r.Set("title", "hi"); st.Insert("Item", r)
			st.MaybeCompact(true)
			st.Close()
			st2, err := OpenStore(db)
			if err != nil || len(st2.All("Item")) != 1 {
				fail("store", fmt.Sprintf("reopen: err=%v n=%d", err, len(st2.All("Item"))))
			} else {
				pass("store", "WAL + compact ok")
				st2.Close()
			}
		}
		os.RemoveAll(dir)
	}

	if ok {
		fmt.Println("\ndoctor: all checks passed")
		return 0
	}
	fmt.Println("\ndoctor: some checks failed")
	return 1
}

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}
