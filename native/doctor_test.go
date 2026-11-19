package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadSrcCRLF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.ae")
	src := "module Hello\r\nentity Item { title: Text }\r\n"
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	got, base, err := readSrc(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "\r") {
		t.Fatalf("CRLF not normalized: %q", got)
	}
	if !strings.Contains(got, "module Hello\nentity") {
		t.Fatalf("unexpected: %q", got)
	}
	if base != dir && filepath.Clean(base) != filepath.Clean(dir) {
		t.Fatalf("base=%q dir=%q", base, dir)
	}
	_, ds := CheckSourceIn(got, base)
	for _, d := range ds {
		if d["severity"] == "error" {
			t.Fatalf("check error: %v", d)
		}
	}
}

func TestChinesePathNewAndCheck(t *testing.T) {
	root := filepath.Join(t.TempDir(), "项目目录")
	os.MkdirAll(root, 0755)
	target := filepath.Join(root, "demo")
	if code := cmdNew([]string{target}, "blank"); code != 0 {
		t.Fatalf("cmdNew exit %d", code)
	}
	main := filepath.Join(target, "main.ae")
	src, base, err := readSrc(main)
	if err != nil {
		t.Fatal(err)
	}
	_, ds := CheckSourceIn(src, base)
	for _, d := range ds {
		if d["severity"] == "error" {
			t.Fatalf("%v", d)
		}
	}
}

func TestStoreCompactPersist(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "t.aedb")
	st, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	st.Ensure(Node{"name": "Item", "fields": []any{Node{"name": "title", "type": Node{"name": "Text"}}}})
	for i := 0; i < 20; i++ {
		r := NewRec(); r.Set("title", "x"); st.Insert("Item", r)
	}
	st.MaybeCompact(true)
	st.Close()
	st2, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if n := len(st2.All("Item")); n != 20 {
		t.Fatalf("got %d want 20", n)
	}
	// file should be compacted (small line count relative to uncompacted)
	if st2.lines > 40 {
		t.Fatalf("expected compacted lines, got %d", st2.lines)
	}
}

func TestDoctorRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		// full doctor writes under LOCALAPPDATA; still fine in CI
	}
	code := cmdDoctor()
	if code != 0 {
		t.Fatalf("doctor exit %d", code)
	}
}

func TestClosestFieldTypo(t *testing.T) {
	src := "entity Account { owner: Text\n balance: Int = 0 }\nfn f() ! db { let x = insert(Account, {owner: \"a\", balanc: 1})\n x }\n"
	_, ds := CheckSource(src)
	found := false
	for _, d := range ds {
		if d["code"] == "E234" && strings.Contains(d["message"].(string), "balanc") {
			found = true
			if h, _ := d["hint"].(string); !strings.Contains(h, "balance") {
				t.Fatalf("hint=%v", d["hint"])
			}
		}
	}
	if !found {
		t.Fatalf("missing E234: %v", ds)
	}
}
