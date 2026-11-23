# Aether 0.4：语言服务器（LSP）、VS Code 扩展与 Aether IDE

v0.4 把 Aether 的语言智能从"专用面板"改为**标准 LSP 语言扩展**：同一个原生二进制里的 `aether lsp`
同时服务于 VS Code / Cursor / VSCodium、品牌发行版 Aether IDE，以及原生 Studio（进程内调用）。

## 1. `aether lsp`
标准输入/输出上的 JSON-RPC 2.0（`Content-Length` 分帧），位置编码 UTF-16。实现：`native/lsp.go`、`native/lsp_analysis.go`。

| 能力 | 方法 | 说明 |
|---|---|---|
| 文档同步 | didOpen / didChange（全量与增量）/ didSave / didClose | 保存时把内容写入内容寻址历史 |
| 诊断 | publishDiagnostics | 检查器的全部规则；精确到出错的名字；E200 自动给出"你是不是想写"建议 |
| 悬停 | textDocument/hover | 实体（字段、不变式、自动生成的 API）、函数/动作签名与契约、内置函数的效果、关键字说明 |
| 补全 | textDocument/completion | 关键字、片段、全局符号、作用域内局部变量、内置函数（含效果）、`.` 后的字段、视图组件 |
| 跳转/引用/重命名 | definition / references / prepareRename / rename | 基于作用域解析，覆盖字符串插值 `"{name}"` 中的引用；拒绝把名字改成关键字 |
| 大纲 | documentSymbol | 模块、实体（字段）、类型（构造器）、函数、动作、视图、流程（步骤）、测试 |
| 格式化 | textDocument/formatting | 与参考实现相同的格式化器 |
| 语义着色 | semanticTokens/full | namespace/class/enum/enumMember/function/method/event/parameter/variable/property/keyword/string/number/operator/comment |
| 代码操作 | codeAction + executeCommand `aether.acceptProposal` | 检查器提示的快速修复；**智能体补丁**（结构化操作，先做类型检查）作为代码操作，接受后记入历史 |

自定义请求（`experimental.aether` 中声明）：
`aether/explain`、`aether/contracts`、`aether/tests`、`aether/run`、`aether/flows`、`aether/runFlow`、
`aether/preview`（返回本机回环地址上的实时预览 URL）、`aether/previewRequest`（直接调用预览应用的 API）、
`aether/history`、`aether/undo`、`aether/redo`、`aether/propose`、`aether/ast`、`aether/agentSchema`。

协议级测试：`tests/test_lsp.py`（9 个用例，直接对原生二进制收发 JSON-RPC）。

## 2. VS Code 扩展（`editors/vscode`）
- TextMate 语法（`syntaxes/aether.tmLanguage.json`，含字符串插值）、语言配置、Aether Dark 主题、文件图标
- `vscode-languageclient` 启动内置的原生二进制（`bin/<平台>/aether lsp`），或设置 `aether.server.path`，或 PATH
- 应用预览 Webview（嵌入 `aether/preview` 的实时地址，编辑后自动刷新）
- 智能体面板（Webview 视图，Codicons 图标）、语义历史树（撤销/重做）、契约树、测试资源管理器（Testing API）、运行程序/流程
- 打包：`editors/vscode/package.sh` → 通用 `aether-vscode-0.6.0.vsix` + 6 个平台专用 .vsix（内置原生二进制）
- 安装：`code --install-extension aether-vscode-0.6.0-linux-x64.vsix`（Cursor/VSCodium 同理，或在扩展视图中"从 VSIX 安装"）
- 无界面集成测试：`test/suite/index.js` 在真实的 VSCodium 1.135 里（Xvfb）运行 14 项检查（激活、诊断、悬停、定义、引用、重命名、补全、大纲、语义着色、格式化、快速修复、智能体代码操作、命令注册、预览）

## 3. Aether IDE（`ide/`）
基于 **VSCodium 1.135.06055 的预编译二进制**（Code - OSS，MIT）改造，**没有**在本机从源码完整编译 VS Code（那需要数小时和大量内存）。`ide/build-ide.sh` 会：
1. 解压 VSCodium；`ide/brand.py` 改写 product.json：名称 Aether IDE、`applicationName=aether-ide`、独立数据目录 `.aether-ide`、URL 协议、Windows AppId/互斥体、macOS Bundle ID，扩展市场为 Open VSX；
2. 替换图标（Linux png、Windows ico、macOS icns）；
3. 把 Aether 扩展作为**内置扩展**放进 `resources/app/extensions/aether-lang.aether-vscode`（带本平台原生二进制），并把智能体视图移到右侧辅助栏；
4. 默认设置：Aether Dark 主题、右侧辅助栏可见、关闭内置第三方 AI 聊天、遥测关闭、命令中心；
5. 打包：Linux `tar.gz`、`.deb`、`AppImage`；Windows 便携 zip（含 `install.bat`/`install.ps1` 按用户安装与快捷方式）；macOS arm64 `.app` zip。

已知限制：Windows `.exe` 内嵌图标未改（需要 rcedit 改 PE 资源）；macOS 包修改后原签名失效（需 `xattr -cr` 或自行签名/公证）；Windows/macOS 包仅在本机构建，未在真机启动验证。

## 4. 原生 Studio（`aether studio`）
标准 IDE 布局：标题栏 + 命令中心、活动栏（资源管理器/搜索/历史/测试/运行/智能体）、侧边栏（文件树 + 大纲）、编辑器标签与面包屑、
预览编辑器组、底部面板（问题/输出/终端/契约/解释）、右侧智能体面板、状态栏、命令面板（Ctrl+Shift+P，`@` 转到符号）。
所有语言智能（诊断、语义着色、悬停、补全、定义、引用、重命名、格式化、代码操作）都通过**进程内的同一个 LSP 服务器**获得。
快捷键：F12 定义、Shift+F12 引用、F2 重命名、Ctrl+. 快速修复/智能体补丁、Ctrl+Space 补全、Ctrl+I 悬停、Ctrl+B/J/Shift+A 切换侧栏/面板/智能体、Ctrl+P 打开文件、Ctrl+` 终端。

## 5. 图标
界面、生成的应用（原生与网页预览）、扩展 Webview 一律使用 **Codicons 字体图标**（CC-BY-4.0，许可见 `native/assets/CODICONS-LICENSE.txt`），不再使用 emoji；CLI 的测试结果改为 `PASS`/`FAIL` 文本。

## 6. v0.5 新增
- **LSP 自定义请求**：
  - `aether/model`：M/V/C 结构
  - `aether/context`（参数 `symbol`）：给智能体的切片
  - `aether/compact`：compact 与 skeleton
  - `aether/flowRuns`、`aether/resumeFlow`
  - `aether/previewRequest`：新增 `dev`（IDE 开发者身份，可绕过登录，HTTP 无法伪造）和 `token` 参数
- **调试**：`aether dap`（stdio 上的 Debug Adapter Protocol），支持断点、继续、单步跳过/进入/跳出、暂停、调用栈、变量（局部/全局）、求值。可调试 run/test/flow 三种模式。F5 调试当前文件，「调试测试」调试测试。
- **扩展命令**：
  - 新建项目（向导）、运行应用（Ctrl+F5）、一键部署
  - 架构（Ctrl+Alt+M）、数据浏览、接口测试
  - 给 Agent 的上下文切片、查看 compact/skeleton 投影
  - 接受智能体的最新建议
- **设置**：
  - `aether.llm.baseUrl/apiKey/model`
  - `aether.projectsDir`
  - `aether.serve.port`、`aether.deploy.port`
  - `aether.agent.revealOnStartup`
- **测试**：
  - 扩展无头测试：`sh editors/vscode/test/runTest.sh`
  - 端到端新手场景：`IDE=<Aether IDE 解压目录> DISPLAY=:77 sh editors/vscode/test/e2e.sh`。该测试在构建好的 Aether IDE 里真实运行，并对屏幕截图。
- **已知**：Aether IDE 的界面语言默认为英文，Aether 自己的视图、命令和教程是中文。如需中文菜单，可以从 Open VSX 安装中文语言包。
