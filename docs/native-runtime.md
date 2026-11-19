# Aether 原生运行时（v0.3）

v0.3 的目标：摆脱"寄生"环境——不再需要 Python 解释器，也不再需要浏览器——用**一个自包含的可执行文件**直接覆盖前端（界面）、后端（HTTP/JSON 接口）和数据库（存储引擎）。

```
aether run|check|test|ui|serve|studio|flow|explain|fmt|hash|ops|propose|history|undo|redo|conform  文件.ae
```

## 1. 总体架构

```
            ┌────────────────────── aether（单个静态二进制，~11 MB）──────────────────────┐
 .ae 源码 → │ lexer.go → parser.go → AST(与 Python JSON IR 同构) → checker.go(类型/效果/契约) │
            │                         ↓                                                    │
            │  interp.go + stdlib.go：求值、契约 requires/ensures/invariant、test/example、 │
            │  flow、allow 权限、spawn/await 并发                                           │
            │      ↓                 ↓                     ↓                                │
            │  store.go          app.go                appview.go / studio_gui.go           │
            │  AeStore(WAL)      HTTP/JSON + OpenAPI    原生渲染：布局、文本、控件            │
            │                    (net/http)             canvas.go → 帧缓冲(RGBA)            │
            │                                               ↓                               │
            │                                   x11.go：手写 X11 线协议 │ tui.go：终端界面   │
            │  agent.go / history.go：结构化操作、补丁、差异、内容寻址历史、撤销/重做        │
            └───────────────────────────────────────────────────────────────────────────────┘
```

| 模块 | 文件 | 说明 |
|---|---|---|
| 语言前端 | lexer.go, parser.go, unparse.go, explain.go | 与 Python 参考实现同一语法；AST 结构一致，语义哈希逐字节相同 |
| 检查器 | checker.go | 完整移植（类型、效果、视图引用、契约） |
| 解释器 | interp.go, values.go, stdlib.go | 树遍历解释器；契约 R040/R041/R042；权限错误；flow；测试 |
| 存储引擎 | store.go | **AeStore**：自研，不是 SQLite（见 §3） |
| 后端 | app.go | 内置 HTTP/JSON 服务器、OpenAPI、内嵌网页预览（可选，非必需） |
| 原生界面 | canvas.go, appview.go, editor.go, x11.go | 自己的布局、文本排版、控件、命中测试；CPU 帧缓冲 |
| 原生 Studio | studio.go, studio_gui.go, tui.go | 编辑器（语法高亮）+ 实时预览 + 诊断/测试/契约/解释/历史/AST + 智能体补丁面板；同时开放智能体 HTTP API（默认 127.0.0.1:7777） |
| 语言服务器 | lsp.go, lsp_analysis.go | v0.4：`aether lsp`（标准 LSP），原生 Studio 与 VS Code 扩展共用 |
| 一致性 | conform.go, conformance/ | 用 Python 参考实现生成的 golden.json 校验原生实现 |

### 规范与一致性
Python 实现保留为**规范参考实现**。`conformance/gen_golden.py` 用它跑 `conformance/programs/*.ae` 与全部 examples，生成 72 个用例（run/check/hash/fmt/explain/test/api/ops/propose）；`aether conform conformance/golden.json` 让原生二进制逐项比对规范化 JSON。当前结果：**72/72 通过**。`tests/test_native.py` 把它纳入 Python 测试套件。

## 2. 原生界面
- Linux/BSD：`aether ui app.ae` 打开**自己的窗口**。我们没有用 GTK/Qt/SDL/Xlib/libc，而是直接通过 unix 套接字讲 X11 协议（认证、建窗、PutImage 推帧、键盘映射、关闭消息）。
- 布局、圆角矩形、文字排版、裁剪、滚动、输入框、按钮、表格、图表、角色切换、流程按钮全部自己画在 RGBA 帧缓冲里。
- 字形光栅化使用 golang.org/x/image 的 OpenType 光栅器；字体是内嵌的 Noto Sans CJK 子集（OFL 授权）。
- 无窗口环境（Windows、macOS、SSH、无 DISPLAY）：自动回退到**终端界面 TUI**（`--tui` 也可强制）。
- `--offscreen WxH --script "...;shot:x.png"` 可无显示器渲染并截图（用于 CI 和智能体自测）。

## 3. 存储引擎 AeStore
- 文件：`<app>.aedb`，每行一条 JSON 操作（put/del/meta），**追加写 + fsync** 构成预写日志（WAL）。
- 打开时重放日志；**撕裂的尾部记录（崩溃时写了一半）被安全忽略**（已测试）。
- 超过 500 行自动压缩：写临时文件 → fsync → 原子 rename。
- 实体结构变化时自动迁移（缺失字段填默认值）。
- 未复用任何数据库库（无 SQLite、无 BoltDB）。

## 4. 诚实声明：哪些是真正独立的，哪些仍然依赖
**自己实现的（本项目代码）**：词法/语法分析、类型与效果检查、解释器、契约与测试系统、流程引擎、权限、操作日志与历史、存储引擎（WAL/恢复/压缩）、X11 线协议客户端、界面布局与控件、文本排版、编辑器、Studio、TUI、智能体补丁/差异算法、一致性测试器。

**仍然依赖的**：
| 层 | 依赖 | 说明 |
|---|---|---|
| 语言运行时 | Go 运行时与标准库 | 垃圾回收、goroutine 调度、net/http、crypto/sha256、encoding/json、os |
| 第三方库 | golang.org/x/image（字体光栅化）、golang.org/x/term（终端原始模式） | 唯一的两个外部库 |
| 图标字体 | Codicons（CC-BY-4.0） | v0.4 起所有界面图标都是字体图标 |
| IDE（可选） | VSCodium/Code - OSS（Electron、Chromium、Node.js） | 仅 Aether IDE 发行版与 VS Code 扩展依赖；原生 Studio 不依赖 |
| 字体 | Noto Sans CJK 子集 | 仅含 GB2312 汉字+ASCII+少量符号；不含 emoji |
| C 库 | **不依赖** | CGO_ENABLED=0 静态编译，`ldd` 显示 "not a dynamic executable" |
| 操作系统内核 | Linux/Windows/macOS 内核系统调用 | 文件、套接字、进程、时间 |
| 显示 | X server（Xorg/Xvfb/XWayland）及其显卡驱动 | 我们只把像素交给 X server；Wayland 原生、Win32、Cocoa 窗口**尚未实现** |
| GPU | **不使用** | 纯 CPU 软件渲染 |
| LLM | 可选的 OpenAI 兼容接口 | 未配置时使用内置 mock 智能体 |

因此：它已脱离 Python 和浏览器，但**没有**脱离操作系统内核、显示服务器和 Go 运行时。

## 5. 平台
| 平台 | 文件 | 原生窗口 | TUI | HTTP 服务/存储/Studio API |
|---|---|---|---|---|
| Linux x86_64 | aether-linux-amd64 | ✓（X11，已实测） | ✓ | ✓（已实测） |
| Linux arm64 | aether-linux-arm64 | ✓（同代码，未在真机测试） | ✓ | ✓ |
| Windows x64/arm64 | aether-windows-*.exe | ✗（回退 TUI） | ✓ | ✓（交叉编译，未在真机测试） |
| macOS x64/arm64 | aether-darwin-* | ✗（XQuartz 下理论可用；默认 TUI） | ✓ | ✓（交叉编译，未签名/未公证，未在真机测试） |

## 6. 路线图：走向自己的操作系统/硬件层
1. **v0.4 补齐窗口后端**：Wayland（wl_shm 协议，手写）、Win32（syscall 调 user32/gdi32）、Cocoa；中文输入法（XIM/IBus/TSF）。
2. **v0.5 直接帧缓冲**：Linux DRM/KMS + evdev 输入，跳过显示服务器——在没有桌面的设备上直接点亮屏幕。
3. **v0.6 编译而非解释**：AST → 字节码 VM → 原生代码（自研后端），去掉 Go 运行时的大部分依赖。
4. **v0.7 Aether 单内核（unikernel）**：把运行时 + 应用 + AeStore 链接成可在虚拟机（virtio 网卡/块设备）里直接启动的镜像，无通用操作系统。
5. **v1.x 裸机**：自研引导、内存管理、调度、驱动（virtio→真实网卡/NVMe/GPU）；最终让"一份 .ae 描述"直接对应到硬件。
每一步都以 `conformance/` 作为不变的验收标准：换了底座，语言语义不能变。
