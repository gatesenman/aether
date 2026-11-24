package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIterationNewSafety(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "existing.ae")
	original := []byte("// 我的源码\nlet x = 42\n")
	os.WriteFile(p, original, 0600)
	if code := cmdNew([]string{p}, "blank"); code == 0 {
		t.Error("existing source overwritten successfully")
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(original) {
		t.Error("existing source changed")
	}
	if code := cmdNew([]string{filepath.Join(d, "missing", "file.ae")}, "blank"); code == 0 {
		t.Error("I/O error reported success")
	}
	blocker := filepath.Join(d, "blocker")
	os.WriteFile(blocker, []byte("keep"), 0600)
	if code := cmdNew([]string{blocker}, "todo"); code == 0 {
		t.Error("file target reported as created project")
	}
	name := filepath.Join(d, "123-中文应用.AE")
	if code := cmdNew([]string{name}, "blank"); code != 0 {
		t.Fatalf("new unicode single file failed %d", code)
	}
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, ds := CheckSourceIn(string(b), d); hasErrors(ds) {
		t.Fatalf("generated invalid module name: %s %+v", b, ds)
	}
}
func TestIterationNewTemplates(t *testing.T) {
	for _, tpl := range Templates {
		t.Run(tpl.ID, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "中文项目")
			if code := cmdNew([]string{dir}, tpl.ID); code != 0 {
				t.Fatal(code)
			}
			b, err := os.ReadFile(filepath.Join(dir, "main.ae"))
			if err != nil {
				t.Fatal(err)
			}
			if _, ds := CheckSourceIn(string(b), dir); hasErrors(ds) {
				t.Fatal(ds)
			}
			for _, name := range []string{"aether.json", "README.md", "AGENTS.md", ".vscode/launch.json", ".gitignore"} {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Error(err)
				}
			}
		})
	}
}
