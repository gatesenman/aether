# Aether —— 人与智能体共用的编程语言（v0.6.0）

一份 `.ae` 文件 = **数据库 + 后端 API + 前端界面**。语言本身覆盖类型、泛型、闭包、代数数据类型、模式匹配、异常、并发、效果系统；
工具链为智能体设计：JSON IR、结构化诊断、结构化补丁协议；配套 Web IDE「Aether Studio」支持人机协作。

## 三条命令上手
```bash
tar xzf aether-0.2.0.tar.gz && cd aether-0.2.0 && ./install.sh     # 安装（创建 ~/.aether 虚拟环境，链接 aether 命令）
aether serve examples/todo.ae                                        # 启动待办应用 → http://127.0.0.1:8080
aether studio examples/todo.ae                                       # 启动 IDE → http://127.0.0.1:7777
```
或：`pip install aether_lang-0.2.0-py3-none-any.whl`（Windows 用 `install.bat`）。仅需 Python 3.9+，无第三方依赖。

## 例子
```aether
module 待办
entity Todo { title: Text  done: Bool = false }
action toggle(t: Todo) { t.done = not t.done }
view 首页 {
  title "我的待办"
  stat "总数" count(Todo)
  form Todo
  list Todo
}
```

## v0.2 新增
契约（requires / ensures / invariant）、内嵌 example 与 test（`aether test`）、内容寻址历史与出处（作者 / 时间 / 原因、撤销 / 重做）、自然语言解释（`aether explain`）、角色权限（`allow`）、可执行流程（`flow`）。范式原则见 **docs/paradigm.md**。

## 文档
- docs/paradigm.md（范式第一性原理）
- docs/01-愿景与百年预测.md
- docs/02-语言规范.md
- docs/03-架构.md
- docs/04-路线图.md（含限制说明）
- docs/05-智能体协议.md
- docs/review-v0.5.md（v0.5 交叉评审与实测）
- docs/mvc.md（Model / View / Controller 对应关系）

## 命令
`aether run | check [--json] | serve | studio | ui | fmt [-w] | ast | ops | propose | test | explain | flow [runs|resume] | history | undo | redo | hash | new [--template] | templates | deploy | mvc | compact | expand | skeleton | context | pkg | lsp | dap | conform | version | migrate | openapi | agents | mcp | errors | doctor`

### 0.6 新增
- `aether explain E200` / `aether errors`：错误码中文解释
- `aether migrate app.ae [--apply]`：schema 与已有数据对账
- `aether openapi app.ae`：带 schema 的 OpenAPI 3.1
- `aether mcp`：给智能体的 MCP 工具；`aether agents app.ae -w`：生成 AGENTS.md
- 三轮自进化记录：[docs/evolution-0.6.md](docs/evolution-0.6.md)

## 测试
`python3 -m unittest discover -s tests`

## v0.3：原生运行时（不再需要 Python 和浏览器）
单个静态可执行文件 `aether`（Go 编写、无 CGO、无 libc 依赖，约 11 MB）内含：解析器/检查器/解释器（含契约、测试、流程、权限、历史）、自研 WAL 存储引擎 AeStore、HTTP/JSON 服务器、原生窗口界面（Linux X11，自研协议客户端与排版）、TUI 回退、原生 Studio（编辑器+预览+诊断+智能体补丁面板）。
- 安装：解压 `release/aether-0.3.0-<平台>.tar.gz|zip`，运行 `install.sh` 或 `install.bat`
- 常用：`aether ui app.ae`、`aether studio app.ae`、`aether serve app.ae`、`aether conform conformance/golden.json`
- 架构与"哪些真正独立/哪些仍依赖"的诚实说明：[docs/native-runtime.md](docs/native-runtime.md)
- Python 实现保留为规范参考实现；一致性套件 72/72 通过。

## v0.4：LSP 语言扩展 + 标准 IDE 布局 + Aether IDE
- `aether lsp`：标准语言服务器（诊断、悬停、补全、定义、引用、重命名、大纲、格式化、语义着色、快速修复与智能体补丁代码操作，以及 explain/history/tests/contracts/flow/preview 自定义请求）
- VS Code / Cursor / VSCodium 扩展：`release/aether-vscode-0.5.0-<平台>.vsix`（内置原生二进制）
- Aether IDE：基于 VSCodium 预编译二进制的品牌发行版（内置 Aether 扩展、Open VSX 市场、默认主题与右侧智能体面板）：`release/Aether-IDE-0.5.0-*`、`aether-ide_0.5.0_amd64.deb`
- 原生 `aether studio` 改为标准 IDE 布局，并在进程内使用同一个 LSP；所有图标改为 Codicons 字体图标，不再使用 emoji
- 说明：[docs/ide-lsp.md](docs/ide-lsp.md)

## v0.5：完整可用（五角色交叉评审 → 实施 → 实测）
评审、对照矩阵、优先级、已实现与推迟项、实测数字：**[docs/review-v0.5.md](docs/review-v0.5.md)**；M/V/C 说明：**[docs/mvc.md](docs/mvc.md)**。
- **新手**：`aether new 我的项目 --template todo|shop|blog|crm|blank`（IDE：「新建项目」向导 + 中文入门 walkthrough）；F5 调试、Ctrl+F5 运行、「一键部署」（`aether deploy`：Dockerfile / systemd / run.sh / run.bat / 本地启动）
- **语言**：字段级类型检查（`a.balanse` → E234 + 拼写建议）；`import "x" as m` 命名空间与 `_私有`；`aether pkg init|add|install [--frozen]|remove|list` + `aether.lock`（sha256）；字段类型推断（`done = false`、`title`、`owner_id`）
- **应用**：`auth { roles…; can <角色> read|write|manage <实体> }` 真实登录（PBKDF2 + 签名令牌）与 401/403；`theme {}`、`row/column/grid/card`、`style {}`、`image`、`link` 布局与样式；可恢复流程 `step "s" retry 3 backoff 200ms`、`aether flow runs|resume`
- **IDE**：Model / Controller / View 三棵树，架构页（ER 图、动作表、流程图、权限矩阵、接口列表），数据浏览，接口测试，DAP 调试器（`aether dap`），智能体面板首次打开不再空白，Windows exe 图标
- **智能体**：真实 LLM 路径（OpenAI 兼容，`AETHER_LLM_BASE_URL/API_KEY/MODEL`）+ 最多 3 轮自我修复；`aether compact|expand|skeleton`（无损往返，AST 哈希不变，注释不保留）；`aether context <文件> <符号>` / LSP `aether/context`（只给相关切片）；基准 `python3 bench/run.py`（tiktoken）
- **原生 Studio**：选择、复制/剪切/粘贴（X11 CLIPBOARD 跨程序）、鼠标悬停提示；中文：粘贴与 keysym 注入可用，系统输入法（XIM）尚不支持
- 端到端新手场景（在 Aether IDE 中，Xvfb 实屏截图）：`DISPLAY=:77 IDE=<解压目录> sh editors/vscode/test/e2e.sh` → `screenshots/v05_*.png`
