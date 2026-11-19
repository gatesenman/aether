# Aether 0.6 连续自进化记录（3 轮）

> 每轮三步：① 行业竞品分析 → ② 开源产品分析 → ③ 反思并选出价值最高的差距，然后**真的改代码、重建、跑测试**。
> 后一轮建立在前一轮之上。本文不含任何编造的性能/基准数字；所有数字都来自本机实际运行的命令输出。

---

## 第 1 轮：错误信息 —— 让人和智能体“看得懂、改得对”

### 发现（带来源）

- **竞品**：2026 年 AI 应用生成器（Lovable、Bolt、v0、Replit Agent）的共同痛点是“修一处、坏两处”的循环，Replit Agent 的优势被普遍归因于**自我纠错循环**（跑 → 读错误 → 修）。自我纠错的质量取决于错误信息的质量。
  来源：<https://appelixir.com/articles/ai-app-builder-comparison-lovable-bolt-v0-replit/>、<https://lovable.dev/guides/bolt-vs-replit-vs-lovable>
- **开源（Gleam）**：我们浅克隆了 `gleam-lang/gleam` 并阅读了 `compiler-core/src/error.rs`（5656 行）和 `diagnostic.rs`。Gleam 的做法是：精确位置、源码片段 + 下划线、简短的一句话说明、可操作的 hint；语言服务器要能容错，一次报出多个真实错误。
  来源：<https://gleam.run/news/context-aware-compilation/>、<https://gleam.run/news/fault-tolerant-gleam/>、<https://github.com/gleam-lang/gleam/blob/v1.18.0-rc2/CHANGELOG.md>
- **错误信息设计原则**：IDE 优先，一两句话；位置最重要；不要把代码里能看到的东西再打印一遍。Elm 的“对话式”错误被反复引用。
  来源：<https://calebmer.com/2019/07/01/writing-good-compiler-error-messages.html>

### 差距（反思）

Aether 0.5.1 已经有稳定错误码（E001…W320、运行时 R001…R099）和“你是不是想写”的提示，但是：

1. 错误码**没有任何解释文档**，人看到 `E234` 不知道去哪查；智能体也拿不到“标准修法”。
2. 命令行输出没有源码片段。
3. LSP 只显示英文短消息。

### 改动

- 新增 `native/errcodes.go`：**全部 57 个错误码**（E/W/R/M）的中文目录：标题、原因、修复示例。
- `aether explain E200`（参数是错误码时解释错误码，是文件时仍是原来的自然语言投影）；`aether errors [--json]` 列出全部。
- `aether check` 文本输出增加源码行（`  2 │ ...`）和“标题；详见 aether explain Exxx”；`--json` 输出增加 `title` 与 `explain` 字段。
- LSP 诊断消息追加中文标题与修法（IDE 悬停即可看到）。
- 新测试 `TestEveryCodeDocumented`：扫描所有 Go 源文件，出现的错误码必须在目录里有条目——防止以后加了错误码却忘了写文档。

### 测试结果

- `go test ./...`：通过（见文末“已知问题”中一个与本轮无关的偶发 X11 剪贴板测试）。
- `aether check` 对 `lenn(1)` 输出：`error E200: undefined name 'lenn' (hint: did you mean 'len'?)` + 源码行 + `未定义的名字；详见 aether explain E200`。

---

## 第 2 轮：数据库演进与 API 契约 —— 改 schema 不再“悄悄坏数据”

### 发现（带来源）

- **Convex**：schema 必须与已有数据一致，不一致就拒绝推送；安全做法是“先放宽 → 迁移数据 → 再收紧”；迁移可断点续跑。
  来源：<https://docs.convex.dev/production/overview>、<https://www.convex.dev/components/migrations>
- **PocketBase（开源，Go 单文件）**：我们浅克隆了 `pocketbase/pocketbase`，阅读 `plugins/migratecmd/automigrate.go`、`migratecmd.go`：集合变更时自动生成迁移文件，`serve` 时在事务中自动执行未应用的迁移。
  来源：<https://pocketbase.io/docs/js-migrations/>
- **Encore**：基础设施和 API 由代码声明，类型化 API 自动生成文档/客户端；本地即可完整运行，方便智能体迭代。
  来源：<https://encore.dev/articles/convex-alternatives>
- **竞品**：Replit Agent 被认为强在“执行数据库迁移并验证”。来源同第 1 轮 Lovable 对比页。

### 差距（反思）

1. AeStore 的“自动迁移”只是把新列显示为空：新字段**不回填默认值**、删掉的字段**残留在数据里**、已有数据**违反新 invariant 也没人知道**。这是第 1 轮之后价值最高的缺口——数据错误比编译错误更难发现。
2. `/api/_openapi` 只有路径，没有字段 schema，外部系统和智能体无法据此生成调用。
3. `aether run` 忽略 `--db`，无法用脚本造数据复现迁移问题。

### 改动

- 新增 `aether migrate <文件> [--db F] [--json] [--apply] [--drop]`（`native/migrate.go`）：
  - **先出计划、只读**；确认后 `--apply` 才写入（走 WAL，并立即压缩）。
  - 新错误码（接入第 1 轮的目录）：M400 回填、M401 必填字段无默认值（阻塞）、M402 字段已删除、M403 已有数据类型或 invariant 不符（阻塞）、M404 实体已删除（需 `--drop`）。
  - 有阻塞项时 `--apply` 拒绝执行，退出码 1——借鉴 Convex 的“schema 必须匹配数据”。
- 新增 `aether openapi <文件>` 与 `/api/_openapi31`：OpenAPI 3.1，带实体 schema（类型、必填、默认值）、`x-aether-invariants`、action 请求体、`x-aether-allow` 角色、403/422 响应。
  - `/api/_openapi` **保持 0.5 的形状不变**：我们第一次直接替换时一致性套件从 72/72 掉到 71/72（第 55 例），所以改成新增端点——这是本轮的一个真实教训。
- `aether run --db F` 生效。
- 新测试 `TestMigratePlanApply`（计划 → 执行 → 再计划为空 → 默认值已回填）。

### 测试结果

- 手工场景（实际输出）：旧数据 2 行 → 新 schema 加 `priority: Int = 3`、删 `old`、加 `invariant len(title) > 1` → 计划显示 M400/M402 和 1 行 M403 阻塞，`--apply` 被拒；放宽 invariant 后 `--apply` 改写 2 行，再次计划“schema 与数据一致”，数据中 `priority` 为 3。
- 一致性套件：72 通过 / 0 失败。

---

## 第 3 轮：智能体接入 —— 从“智能体友好的语法”到“智能体可直接调用的工具”

### 发现（带来源）

- **Unison 1.0**：UCM 内置 MCP 服务器（`ucm mcp`），提供 typecheck、docs、依赖查看、测试等工具。
  来源：<https://www.unison-lang.org/docs/usage-topics/mcp-setup/>、<https://www.unison-lang.org/unison-1-0/>
- **Darklang（2026 新方向）**：面向智能体的语言/平台，内容寻址定义、语义级源码管理、给智能体的 CLI 文档与执行轨迹。
  来源：<https://wip.darklang.com/cli>、<https://wip.darklang.com/source-control>
- **Wasp（开源）**：2026-06 放弃自研 DSL 改用 TypeScript spec，理由是自研语言需要自己的 IDE 扩展与生态；同时推出智能体插件，把最佳实践写进 `AGENTS.md` / `CLAUDE.md`。对 Aether 的启示：自研语言必须**主动**把工具链暴露给通用智能体，否则会被“直接用 TS”替代。
  来源：<https://wasp.sh/blog/2026/06/15/wasp-typescript-spec>、<https://wasp.sh/docs/wasp-ai/coding-agent-plugin>
- **AGENTS.md**：跨工具的开放格式（Cursor、Windsurf、Codex、Devin 等都支持），最近的文件优先。
  来源：<https://agents.md/>、<https://cursor.com/docs/rules>、<https://docs.devin.ai/cli/extensibility/rules>

### 差距（反思）

Aether 已有 `compact/skeleton/context/ops` 等智能体友好命令，但只能被**知道 Aether 的**智能体通过命令行调用。Cursor、Windsurf 等通用智能体进入一个 Aether 项目时，既没有项目说明，也没有标准协议接入。第 1、2 轮产出的错误码目录和迁移计划，正好可以成为智能体工具。

### 改动

- 新增 `aether mcp`（`native/mcp.go`）：MCP stdio 服务器，**只读**工具 7 个：
  `aether_check`、`aether_test`、`aether_skeleton`、`aether_context`、`aether_explain_code`（第 1 轮）、`aether_migrate_plan`（第 2 轮）、`aether_openapi`（第 2 轮）。写操作仍走 `aether ops`（有历史、可撤销），不通过 MCP 暴露。
- 新增 `aether agents <文件> [-w]`：从源码推导出 AGENTS.md（验证命令、修改约定、实体、action/flow、签名骨架），`aether new` 创建项目时自动生成。
- VS Code 扩展 / Aether IDE：注册 MCP 服务器定义（宿主支持 `lm.registerMcpServerDefinitionProvider` 时才启用，旧版本静默跳过）；新增命令“解释错误码”“生成 AGENTS.md”“数据迁移计划”（字体图标 `$(question)` `$(hubot)` `$(database)`）。
- 新测试 `TestOpenAPIAgentsMCP`（OpenAPI 可序列化且必填正确、AGENTS.md 内容、MCP tools/list ≥ 7、tools/call 解释 M401、通知不回复）。

### 测试结果

- 用管道向 `aether mcp` 发送 initialize / tools/list / tools/call 实测：返回协议版本、7 个工具；`aether_check` 返回带 `title`/`explain` 的 E200 诊断；`aether_test` 对 todo 模板返回全部通过。
- **未在真实的 Cursor / VS Code 智能体会话中验证 MCP 接入**；扩展里的 MCP 注册未在 GUI 中实测。

---

## 0.6.0 最终验证（本机实测）

| 项 | 结果 |
|---|---|
| `go test ./...` | 通过；`TestX11ClipboardCrossClient` 偶发失败（本机约 17 次完整运行中失败 3 次；单独连跑 10 次全部通过；用 -skip 跳过它后其余测试全部通过），与本次改动无关，疑为共享桌面剪贴板竞争 |
| todo 模板 `check` / `test` | `main.ae: ok`；4 通过 0 失败 |
| 其他模板 blank/shop/blog/crm `check` | 全部 ok |
| 一致性套件 | 72 / 72 |
| Windows `aether.exe`（box 上用 Wine 运行，不是真机） | `version` 0.6.0；todo 模板 `new`/`check`/`test` 4/4 通过；`explain M401` 正常 |
| Setup.exe | 结构校验：载荷 zip 完整（2266 个条目，testzip 无错），内置扩展含 MCP 注册；**未在真机运行** |
| 交叉编译 | linux/windows/darwin × amd64/arm64 全部成功 |

## 仍然存在的限制

- Setup.exe 仍未代码签名，SmartScreen 可能拦截。
- Aether IDE 的 Electron 外壳仍是 0.5.0 的 VSCodium 换皮，0.6.0 只替换内置扩展与 `aether.exe`。
- Windows / macOS 仍无原生 GUI 窗口；原生 Studio 无中文输入法。
- `migrate` 是单机 AeStore 的离线迁移，没有 Convex 那样的在线分批、断点续跑。
- MCP 只读；真实大模型路径与 MCP 在第三方智能体里的接入未实测。
- 0.6.0 安装包未在用户真机上测试。
