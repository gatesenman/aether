package main

// Modules & package manager basics (v0.5).
//   import "lib/money"            -- flat include (v0.1 behaviour)
//   import "lib/money" as money   -- namespaced: money.fee(1); names starting with _ are private
//   import "greet"                -- resolved from aether_modules/greet/main.ae (installed by `aether pkg`)

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ResolveImport finds the file for an import path relative to base.
func ResolveImport(base, p string) (string, error) {
	p = strings.TrimPrefix(p, "pkg:")
	cands := []string{}
	add := func(x string) {
		if !strings.HasSuffix(x, ".ae") {
			cands = append(cands, x+".ae", filepath.Join(x, "main.ae"))
		} else {
			cands = append(cands, x)
		}
	}
	add(filepath.Join(base, p))
	// package lookup: walk up to find aether_modules
	parts := strings.SplitN(filepath.ToSlash(p), "/", 2)
	for d := base; ; d = filepath.Dir(d) {
		md := filepath.Join(d, "aether_modules", parts[0])
		if st, err := os.Stat(md); err == nil && st.IsDir() {
			if len(parts) == 2 {
				add(filepath.Join(md, parts[1]))
			} else {
				if m := manifestMain(md); m != "" {
					cands = append(cands, filepath.Join(md, m))
				}
				cands = append(cands, filepath.Join(md, "main.ae"), filepath.Join(md, parts[0]+".ae"))
			}
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("cannot import %s (looked in %s and aether_modules/)", p, base)
}

func manifestMain(dir string) string {
	var m Manifest
	if b, err := os.ReadFile(filepath.Join(dir, "aether.json")); err == nil && json.Unmarshal(b, &m) == nil {
		return m.Main
	}
	return ""
}

func (it *Interp) importModule(d Node) {
	file, err := ResolveImport(it.BaseDir, d["path"].(string))
	if err != nil {
		panic(aeErr("R014", ln(d), "%s", err.Error()))
	}
	data, err := os.ReadFile(file)
	if err != nil {
		panic(aeErr("R014", ln(d), "cannot import %s", file))
	}
	sub, perr := Parse(string(data))
	if perr != nil {
		panic(perr)
	}
	alias, _ := d["alias"].(string)
	if it.loaded == nil {
		it.loaded = map[string]*Record{}
	}
	abs, _ := filepath.Abs(file)
	if alias == "" {
		if _, seen := it.loaded[abs]; seen {
			return
		}
		it.loaded[abs] = nil
		oldBase := it.BaseDir
		it.BaseDir = filepath.Dir(file)
		for _, sd := range sub["decls"].([]any) {
			it.declare(sd.(Node))
		}
		it.BaseDir = oldBase
		return
	}
	if m := it.loaded[abs]; m != nil {
		it.Globals.Define(alias, m, false)
		return
	}
	env := NewEnv(it.Globals)
	saved, oldBase := it.declEnv, it.BaseDir
	it.declEnv, it.BaseDir = env, filepath.Dir(file)
	for _, sd := range sub["decls"].([]any) {
		sn := sd.(Node)
		switch sn["k"] {
		case "Module", "Import", "Entity", "TypeDecl", "Fn", "Action", "View", "Flow", "Test", "Example":
			if sn["k"] == "Test" || sn["k"] == "Example" || sn["k"] == "View" {
				continue // a library's tests/views do not leak into the importer
			}
			it.declare(sn)
		default:
			it.exec(sn, env)
		}
	}
	it.declEnv, it.BaseDir = saved, oldBase
	rec := NewRec()
	names := []string{}
	for k := range env.vars {
		if !strings.HasPrefix(k, "_") {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		rec.Set(k, env.vars[k])
	}
	it.loaded[abs] = rec
	it.Globals.Define(alias, rec, false)
}

// ModuleExports parses an imported module (for the checker): exported names and entities.
func ModuleExports(base, path string) (names map[string]bool, prog Node, err error) {
	file, err := ResolveImport(base, path)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}
	prog, err = Parse(string(data))
	if err != nil {
		return nil, nil, err
	}
	names = map[string]bool{}
	for _, x := range prog["decls"].([]any) {
		d := x.(Node)
		if n, ok := d["name"].(string); ok && n != "" && !strings.HasPrefix(n, "_") {
			names[n] = true
		}
		if d["k"] == "TypeDecl" {
			for _, v := range d["variants"].([]any) {
				names[v.(Node)["name"].(string)] = true
			}
		}
	}
	return names, prog, nil
}

// ---------------- package manager ----------------

type Manifest struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Main         string            `json:"main,omitempty"`
	Description  string            `json:"description,omitempty"`
	Dependencies map[string]string `json:"dependencies"`
}
type LockEntry struct {
	Source string `json:"source"`
	SHA256 string `json:"sha256"`
	Time   string `json:"installed"`
}

func readManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "aether.json"))
	if err != nil {
		return nil, fmt.Errorf("没有找到 aether.json（先运行 aether pkg init）")
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Dependencies == nil {
		m.Dependencies = map[string]string{}
	}
	return &m, nil
}
func writeJSONFile(path string, v any) error {
	b, _ := json.MarshalIndent(v, "", "  ")
	return os.WriteFile(path, append(b, '\n'), 0644)
}
func readLock(dir string) map[string]LockEntry {
	l := map[string]LockEntry{}
	if b, err := os.ReadFile(filepath.Join(dir, "aether.lock")); err == nil {
		json.Unmarshal(b, &l)
	}
	return l
}

// treeHash: sha256 over sorted relative paths + contents (ignores .git).
func treeHash(dir string) string {
	files := []string{}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			r, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(r))
		}
		return nil
	})
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		fmt.Fprintf(h, "%s\x00%d\x00", f, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(src, p)
		if info.IsDir() && (info.Name() == ".git" || info.Name() == "aether_modules" || info.Name() == "dist") && r != "." {
			return filepath.SkipDir
		}
		t := filepath.Join(dst, r)
		if info.IsDir() {
			return os.MkdirAll(t, 0755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(t, b, 0644)
	})
}

func untarGz(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(h.Name)
		// strip a single top-level directory, refuse path traversal
		if i := strings.IndexByte(filepath.ToSlash(name), '/'); i >= 0 {
			name = name[i+1:]
		} else if h.Typeflag == tar.TypeDir {
			continue
		}
		if name == "" || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		t := filepath.Join(dst, name)
		switch h.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(t, 0755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(t), 0755)
			f, err := os.Create(t)
			if err != nil {
				return err
			}
			io.Copy(f, io.LimitReader(tr, 64<<20))
			f.Close()
		}
	}
}

// fetchPackage installs source into dst. Sources: local dir, .tar.gz file/URL, git URL.
func fetchPackage(source, dst, root string) error {
	os.RemoveAll(dst)
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	switch {
	case strings.HasPrefix(source, "git+") || strings.HasSuffix(source, ".git"):
		u := strings.TrimPrefix(source, "git+")
		os.RemoveAll(dst)
		out, err := exec.Command("git", "clone", "--depth", "1", u, dst).CombinedOutput()
		if err != nil {
			return fmt.Errorf("git clone 失败: %s", out)
		}
		os.RemoveAll(filepath.Join(dst, ".git"))
		return nil
	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		cl := http.Client{Timeout: 60 * time.Second}
		resp, err := cl.Get(source)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
		}
		return untarGz(resp.Body, dst)
	default:
		p := source
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		st, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("找不到包来源 %s", source)
		}
		if st.IsDir() {
			return copyTree(p, dst)
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		return untarGz(f, dst)
	}
}

func pkgInstallOne(root, name, source string, lock map[string]LockEntry, strict bool) error {
	dst := filepath.Join(root, "aether_modules", name)
	if err := fetchPackage(source, dst, root); err != nil {
		return err
	}
	h := treeHash(dst)
	if old, ok := lock[name]; ok && old.Source == source && old.SHA256 != h {
		if strict {
			return fmt.Errorf("包 %s 的内容与 aether.lock 记录不一致（%s… ≠ %s…）", name, old.SHA256[:12], h[:12])
		}
		fmt.Printf("警告：%s 内容已变化，更新锁文件\n", name)
	}
	lock[name] = LockEntry{source, h, time.Now().Format(time.RFC3339)}
	return nil
}

func cmdPkg(args []string) int {
	root, _ := os.Getwd()
	if len(args) == 0 {
		fmt.Println("用法: aether pkg init [名称] | add <名称> <来源> | install [--frozen] | remove <名称> | list\n来源可以是本地目录、.tar.gz 文件或网址、git 仓库（git+https://… 或 *.git）")
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(os.Stderr, "错误:", err); return 1 }
	switch args[0] {
	case "init":
		name := filepath.Base(root)
		if len(args) > 1 {
			name = args[1]
		}
		if _, err := os.Stat(filepath.Join(root, "aether.json")); err == nil {
			return fail(fmt.Errorf("aether.json 已存在"))
		}
		main := "main.ae"
		if _, err := os.Stat(filepath.Join(root, main)); err != nil {
			if ms, _ := filepath.Glob(filepath.Join(root, "*.ae")); len(ms) > 0 {
				main = filepath.Base(ms[0])
			}
		}
		writeJSONFile(filepath.Join(root, "aether.json"), Manifest{Name: name, Version: "0.1.0", Main: main, Dependencies: map[string]string{}})
		fmt.Println("已创建 aether.json")
	case "add":
		if len(args) < 3 {
			return fail(fmt.Errorf("用法: aether pkg add <名称> <来源>"))
		}
		m, err := readManifest(root)
		if err != nil {
			return fail(err)
		}
		lock := readLock(root)
		delete(lock, args[1])
		if err := pkgInstallOne(root, args[1], args[2], lock, false); err != nil {
			return fail(err)
		}
		m.Dependencies[args[1]] = args[2]
		writeJSONFile(filepath.Join(root, "aether.json"), m)
		writeJSONFile(filepath.Join(root, "aether.lock"), lock)
		fmt.Printf("已安装 %s（sha256 %s…）。在代码中使用：import \"%s\" as %s\n", args[1], lock[args[1]].SHA256[:12], args[1], args[1])
	case "install":
		m, err := readManifest(root)
		if err != nil {
			return fail(err)
		}
		lock := readLock(root)
		strict := len(args) > 1 && args[1] == "--frozen"
		names := []string{}
		for n := range m.Dependencies {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if err := pkgInstallOne(root, n, m.Dependencies[n], lock, strict); err != nil {
				return fail(err)
			}
			fmt.Println("已安装", n)
		}
		writeJSONFile(filepath.Join(root, "aether.lock"), lock)
	case "remove":
		if len(args) < 2 {
			return fail(fmt.Errorf("用法: aether pkg remove <名称>"))
		}
		m, err := readManifest(root)
		if err != nil {
			return fail(err)
		}
		delete(m.Dependencies, args[1])
		lock := readLock(root)
		delete(lock, args[1])
		os.RemoveAll(filepath.Join(root, "aether_modules", args[1]))
		writeJSONFile(filepath.Join(root, "aether.json"), m)
		writeJSONFile(filepath.Join(root, "aether.lock"), lock)
		fmt.Println("已移除", args[1])
	case "list":
		m, err := readManifest(root)
		if err != nil {
			return fail(err)
		}
		lock := readLock(root)
		fmt.Printf("%s %s\n", m.Name, m.Version)
		for n, s := range m.Dependencies {
			h := lock[n].SHA256
			if len(h) > 12 {
				h = h[:12]
			}
			fmt.Printf("  %s  %s  %s\n", n, s, h)
		}
	default:
		return fail(fmt.Errorf("未知子命令 %s", args[0]))
	}
	return 0
}
