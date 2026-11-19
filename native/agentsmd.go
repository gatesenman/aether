package main

// AGENTS.md 生成（0.6 第 3 轮）：遵循 agents.md 开放格式（Cursor/Windsurf/Codex/Devin 等通用）。
// 内容由源码推导（实体/action/视图/流程 + 验证命令），不是手写说明，避免与代码漂移（Wasp spec 的思路）。

import (
	"fmt"
	"sort"
	"strings"
)

func AgentsMD(src, file string) (string, error) {
	it := NewInterp(":memory:", false)
	if err := it.Load(src, "."); err != nil {
		return "", err
	}
	defer it.Store.Close()
	sk, _ := Skeleton(src)
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("# AGENTS.md — %s", it.Module)
	w("")
	w("本项目用 Aether 语言编写：一份 `%s` 同时描述数据库（entity）、后端（action/flow）和界面（view）。", file)
	w("本文件由 `aether agents %s -w` 生成，改了源码后请重新生成。", file)
	w("")
	w("## 验证命令（每次修改后都要跑）")
	w("")
	w("- 静态检查：`aether check %s`（错误码说明：`aether explain E200`）", file)
	w("- 测试：`aether test %s`", file)
	w("- 数据迁移计划：`aether migrate %s`（确认后 `--apply`）", file)
	w("- 运行界面：`aether ui %s`；HTTP API：`aether serve %s`（`aether openapi %s` 导出接口描述）", file, file, file)
	w("")
	w("## 修改方式")
	w("")
	w("- 先读骨架 `aether skeleton %s`，再取切片 `aether context %s <符号>`，不要整文件通读。", file, file)
	w("- 优先用结构化编辑 `aether ops %s '<json>' -w --author agent --reason \"…\"`，它会记录历史，可 `aether undo`。", file)
	w("- 有副作用的函数要声明 `! db` / `! io`；新增字段要给默认值，否则迁移会报 M401。")
	w("- 业务规则写成 `invariant` / `requires` / `ensures`，并补一个 `test` 或 `example`。")
	w("- 图标用字体图标（Codicons 名称），不要用 emoji。")
	w("- MCP：智能体可直接接入 `aether mcp`（stdio）。")
	w("")
	w("## Model（实体）")
	w("")
	for _, e := range it.EntOrder {
		fs := []string{}
		for _, f := range it.Entities[e]["fields"].([]any) {
			fn := f.(Node)
			fs = append(fs, fn["name"].(string)+": "+fn["type"].(Node)["name"].(string))
		}
		w("- `%s` { %s }", e, strings.Join(fs, ", "))
	}
	w("")
	w("## Controller（action / flow）")
	w("")
	for _, n := range it.ActOrder {
		a := it.Actions[n]
		extra := ""
		if al, _ := a["allow"].([]any); len(al) > 0 {
			ss := []string{}
			for _, r := range al {
				ss = append(ss, fmt.Sprint(r))
			}
			extra = "（仅 " + strings.Join(ss, ", ") + "）"
		}
		w("- action `%s`%s", n, extra)
	}
	fl := []string{}
	for n := range it.Flows {
		fl = append(fl, n)
	}
	sort.Strings(fl)
	for _, n := range fl {
		w("- flow `%s`（持久化，可 `aether flow resume`）", n)
	}
	w("")
	w("## 签名骨架")
	w("")
	w("```aether")
	w("%s", strings.TrimRight(sk, "\n"))
	w("```")
	return b.String(), nil
}
