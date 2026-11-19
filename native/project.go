package main

// Project templates (`aether new`), deployment bundles (`aether deploy`) and the MVC report (`aether mvc`).

import (
	"embed"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed templates
var templatesFS embed.FS

type Template struct{ ID, Title, Desc string }

var Templates = []Template{
	{"blank", "空白项目", "一个实体、一个动作、一个页面，适合从零开始"},
	{"todo", "待办清单", "最经典的入门示例：增删改查、统计、流程"},
	{"shop", "小店（登录与权限）", "账号登录、角色权限、主题与布局、可重试流程"},
	{"blog", "博客", "文章与评论，展示多个实体与页面"},
	{"crm", "客户管理（MVC 示例）", "清楚展示 Model / Controller / View 三层，含关系、权限、流程"},
}

func tutorial(t Template, name string) string {
	return "# " + name + "\n\n由模板「" + t.Title + "」创建：" + t.Desc + "。\n\n" +
		`## 五分钟上手

1. **看懂结构**：打开 main.ae。Aether 程序分三层（左侧「架构」视图可以图形化查看）：
   - **Model 模型** —— ` + "`entity`" + `：要保存哪些数据、字段类型、必须满足的规则（invariant）。
   - **Controller 控制** —— ` + "`action` / `flow` / `auth`" + `：能对数据做什么、谁能做、多步流程。
   - **View 界面** —— ` + "`view` / `theme`" + `：页面上显示什么、怎么排版。
2. **预览**：按 Ctrl+Shift+V，右边出现可以直接操作的应用。
3. **让智能体修改**：在右侧「Aether 智能体」里用中文说需求，比如「给 Item 添加字段 note: Text」，看差异，点「接受」。
4. **运行与测试**：F5 运行；左侧烧瓶图标运行测试。
5. **调试**：在行号左边点一下设置断点，选择「Aether: 调试」后按 F5，可以单步执行、查看变量。
6. **数据与接口**：左侧「架构」视图 → 「数据浏览」查看保存的记录，「接口测试」直接调用 API。
7. **部署**：命令面板运行「Aether: 一键部署」，或在终端执行 ` + "`aether deploy main.ae`" + `，得到可以直接运行的部署包（含 Dockerfile）。

## 常用命令

| 命令 | 作用 |
|---|---|
| aether ui main.ae | 原生窗口运行应用 |
| aether serve main.ae --port 8080 | 作为网站 / API 运行 |
| aether test main.ae | 运行测试 |
| aether mvc main.ae | 打印 MVC 架构摘要 |
| aether deploy main.ae | 生成部署包 |
| aether pkg add 名称 来源 | 安装依赖包 |
`
}

func cmdNew(pos []string, tpl string) int {
	if len(pos) < 1 {
		fmt.Println("用法: aether new <项目目录> [--template blank|todo|shop|blog|crm]")
		for _, t := range Templates {
			fmt.Printf("  %-6s %s — %s\n", t.ID, t.Title, t.Desc)
		}
		return 2
	}
	target := filepath.Clean(pos[0])
	if strings.HasSuffix(strings.ToLower(target), ".ae") { // v0.1 behaviour: single file
		tpl = "blank"
		b, _ := templatesFS.ReadFile("templates/blank/main.ae")
		name := strings.TrimSuffix(filepath.Base(target), ".ae")
		os.WriteFile(target, []byte(strings.Replace(string(b), "module 我的应用", "module "+name, 1)), 0644)
		fmt.Println("created", target, "->  aether ui", target)
		return 0
	}
	var t *Template
	for i := range Templates {
		if Templates[i].ID == tpl {
			t = &Templates[i]
		}
	}
	if t == nil {
		fmt.Fprintln(os.Stderr, "未知模板", tpl)
		return 2
	}
	if ents, _ := os.ReadDir(target); len(ents) > 0 {
		fmt.Fprintln(os.Stderr, "目录不为空:", target)
		return 1
	}
	os.MkdirAll(filepath.Join(target, ".vscode"), 0755)
	name := filepath.Base(target)
	fs.WalkDir(templatesFS, "templates/"+t.ID, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, _ := templatesFS.ReadFile(p)
		os.WriteFile(filepath.Join(target, strings.TrimPrefix(p, "templates/"+t.ID+"/")), b, 0644)
		return nil
	})
	writeJSONFile(filepath.Join(target, "aether.json"), Manifest{Name: name, Version: "0.1.0", Main: "main.ae", Description: t.Title, Dependencies: map[string]string{}})
	os.WriteFile(filepath.Join(target, "README.md"), []byte(tutorial(*t, name)), 0644)
	os.WriteFile(filepath.Join(target, ".vscode", "launch.json"), []byte(`{
  "version": "0.2.0",
  "configurations": [
    {"type": "aether", "request": "launch", "name": "Aether: 调试运行", "program": "${workspaceFolder}/main.ae", "mode": "run"},
    {"type": "aether", "request": "launch", "name": "Aether: 调试测试", "program": "${workspaceFolder}/main.ae", "mode": "test"}
  ]
}
`), 0644)
	if mb, err := os.ReadFile(filepath.Join(target, "main.ae")); err == nil {
		if md, err := AgentsMD(string(mb), "main.ae"); err == nil {
			os.WriteFile(filepath.Join(target, "AGENTS.md"), []byte(md), 0644)
		}
	}
	os.WriteFile(filepath.Join(target, ".gitignore"), []byte("*.aedb\n*.history.json\naether_modules/\ndist/\n"), 0644)
	fmt.Printf("已创建项目 %s（模板：%s）\n  下一步：cd %s && aether ui main.ae   或用 Aether IDE 打开该文件夹\n", target, t.Title, target)
	return 0
}

func freePort(p int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// cmdDeploy builds a self-contained deployment bundle; --target local also starts it in the background.
func cmdDeploy(file, out, target string, port int) int {
	src, dir, err := readSrc(file)
	if err != nil {
		return printErr(file, err)
	}
	if _, ds := CheckSourceIn(src, dir); hasErrors(ds) {
		for _, d := range ds {
			if d["severity"] == "error" {
				fmt.Fprintf(os.Stderr, "%s:%v: error %v: %v\n", file, d["line"], d["code"], d["message"])
			}
		}
		fmt.Fprintln(os.Stderr, "存在错误，已取消部署")
		return 1
	}
	if out == "" {
		out = filepath.Join(dir, "dist", "deploy")
	}
	os.RemoveAll(out)
	os.MkdirAll(filepath.Join(out, "app"), 0755)
	// app sources (+ modules)
	copyTree(dir, filepath.Join(out, "app"))
	os.RemoveAll(filepath.Join(out, "app", "dist"))
	filepath.Walk(filepath.Join(out, "app"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && (strings.HasSuffix(p, ".aedb") || strings.HasSuffix(p, ".history.json")) {
			os.Remove(p)
		}
		return nil
	})
	if d := filepath.Join(dir, "aether_modules"); dirExists(d) {
		copyTree(d, filepath.Join(out, "app", "aether_modules"))
	}
	self, _ := os.Executable()
	bin := "aether"
	if runtime.GOOS == "windows" {
		bin = "aether.exe"
	}
	if b, err := os.ReadFile(self); err == nil {
		os.WriteFile(filepath.Join(out, bin), b, 0755)
	}
	main := filepath.Base(file)
	os.WriteFile(filepath.Join(out, "run.sh"), []byte("#!/bin/sh\ncd \"$(dirname \"$0\")\"\nPORT=${PORT:-"+fmt.Sprint(port)+"}\nexec ./aether serve app/"+main+" --host 0.0.0.0 --port $PORT --db ${AETHER_DB:-data/app.aedb}\n"), 0755)
	os.WriteFile(filepath.Join(out, "run.bat"), []byte("@echo off\r\ncd /d %~dp0\r\nif not exist data mkdir data\r\naether.exe serve app\\"+main+" --host 0.0.0.0 --port "+fmt.Sprint(port)+" --db data\\app.aedb\r\n"), 0644)
	os.MkdirAll(filepath.Join(out, "data"), 0755)
	os.WriteFile(filepath.Join(out, "Dockerfile"), []byte("# 构建: docker build -t my-aether-app .   运行: docker run -p "+fmt.Sprint(port)+":"+fmt.Sprint(port)+" -v appdata:/srv/data my-aether-app\nFROM scratch\nCOPY aether /aether\nCOPY app /srv/app\nWORKDIR /srv\nVOLUME /srv/data\nEXPOSE "+fmt.Sprint(port)+"\nENTRYPOINT [\"/aether\", \"serve\", \"app/"+main+"\", \"--host\", \"0.0.0.0\", \"--port\", \""+fmt.Sprint(port)+"\", \"--db\", \"/srv/data/app.aedb\"]\n"), 0644)
	os.WriteFile(filepath.Join(out, "aether-app.service"), []byte("[Unit]\nDescription=Aether app "+main+"\nAfter=network.target\n\n[Service]\nWorkingDirectory=/opt/aether-app\nExecStart=/opt/aether-app/run.sh\nRestart=always\nEnvironment=PORT="+fmt.Sprint(port)+"\n\n[Install]\nWantedBy=multi-user.target\n"), 0644)
	os.WriteFile(filepath.Join(out, "部署说明.md"), []byte("# 部署说明\n\n这个文件夹可以整体复制到任何服务器（与构建机相同的操作系统/架构，或用 Docker）。\n\n- 直接运行：`./run.sh`（Windows：`run.bat`），浏览器打开 http://服务器:"+fmt.Sprint(port)+"/\n- Docker：`docker build -t my-app . && docker run -p "+fmt.Sprint(port)+":"+fmt.Sprint(port)+" -v appdata:/srv/data my-app`\n- 开机自启（Linux）：复制到 /opt/aether-app，`sudo cp aether-app.service /etc/systemd/system/ && sudo systemctl enable --now aether-app`\n- 数据保存在 data/app.aedb（追加日志 + 自动压缩），备份这个文件即可。\n- 如果程序声明了 `auth`，第一个注册的账号自动成为管理员。\n"), 0644)
	fmt.Println("部署包已生成:", out)
	if target == "local" {
		for !freePort(port) {
			port++
		}
		os.MkdirAll(filepath.Join(out, "data"), 0755)
		cmd := exec.Command(filepath.Join(out, bin), "serve", filepath.Join(out, "app", main), "--port", fmt.Sprint(port), "--db", filepath.Join(out, "data", "app.aedb"))
		lf, _ := os.Create(filepath.Join(out, "server.log"))
		cmd.Stdout, cmd.Stderr = lf, lf
		detach(cmd)
		if err := cmd.Start(); err != nil {
			return printErr(file, err)
		}
		os.WriteFile(filepath.Join(out, "server.pid"), []byte(fmt.Sprint(cmd.Process.Pid)), 0644)
		for i := 0; i < 50 && freePort(port); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Printf("已在本机启动: http://127.0.0.1:%d/  (pid %d，停止: kill %d)\n", port, cmd.Process.Pid, cmd.Process.Pid)
	}
	return 0
}
func dirExists(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
func hasErrors(ds []Diag) bool {
	for _, d := range ds {
		if d["severity"] == "error" {
			return true
		}
	}
	return false
}

func cmdMVC(file string, asJSON bool) int {
	src, dir, err := readSrc(file)
	if err != nil {
		return printErr(file, err)
	}
	m := ArchModel(src, dir)
	if asJSON {
		fmt.Println(toJSON(m))
		return 0
	}
	if m["model"] == nil {
		fmt.Println(toJSON(m))
		return 1
	}
	fmt.Println("== Model（模型：数据） ==")
	for _, e := range m["model"].(map[string]any)["entities"].([]any) {
		en := e.(map[string]any)
		fs := []string{}
		for _, f := range en["fields"].([]any) {
			fm := f.(map[string]any)
			s := fmt.Sprintf("%v: %v", fm["name"], fm["type"])
			if r, ok := fm["ref"]; ok {
				s += " → " + r.(string)
			}
			fs = append(fs, s)
		}
		fmt.Printf("  entity %v { %s }\n", en["name"], strings.Join(fs, ", "))
		for _, iv := range en["invariants"].([]any) {
			fmt.Printf("      invariant %v\n", iv)
		}
	}
	c := m["controller"].(map[string]any)
	fmt.Println("== Controller（控制：动作、流程、权限） ==")
	for _, a := range c["actions"].([]any) {
		am := a.(map[string]any)
		fmt.Printf("  action %v  %v  allow=%v  touches=%v\n", am["name"], am["endpoint"], am["allow"], am["touches"])
	}
	for _, f := range c["flows"].([]any) {
		fm := f.(map[string]any)
		ss := []string{}
		for _, s := range fm["steps"].([]any) {
			ss = append(ss, fmt.Sprint(s.(map[string]any)["name"]))
		}
		fmt.Printf("  flow %v: %s\n", fm["name"], strings.Join(ss, " → "))
	}
	if au, ok := c["auth"].(map[string]any); ok && au != nil {
		fmt.Printf("  auth roles=%v login=%v signup=%v\n", au["roles"], au["login"], au["signup"])
	}
	fmt.Println("== View（界面） ==")
	for _, v := range m["view"].(map[string]any)["views"].([]any) {
		vm := v.(map[string]any)
		fmt.Printf("  view %v  uses=%v  buttons=%v\n", vm["name"], vm["entities"], vm["actions"])
	}
	return 0
}
