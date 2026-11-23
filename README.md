<p align="center"><img src="editors/vscode/media/aether.png" alt="Aether Language" width="128"></p>

# Aether 语言

**0.6.0** · 人与智能体共用的编程语言、运行时与 Aether IDE：一份 `.ae` 文件同时描述数据库、后端 API 和前端界面。

## 下载（Windows x64，0.6.0）

正式发布页：[GitHub Releases v0.6.0](https://github.com/gatesenman/aether/releases/tag/v0.6.0)

| 包 | 用途 | 下载 |
|---|---|---|
| `Aether-Setup-0.6.0-win32-x64.exe` | 官方 NSIS 图形安装向导（推荐） | [下载 Setup](https://github.com/gatesenman/aether/releases/download/v0.6.0/Aether-Setup-0.6.0-win32-x64.exe) |
| `Aether-IDE-0.6.0-win32-x64.zip` | 便携包：解压后运行 `Aether IDE.exe`，或配合 `install.bat` | [下载便携包](https://github.com/gatesenman/aether/releases/download/v0.6.0/Aether-IDE-0.6.0-win32-x64.zip) |
| `aether-0.6.0-windows-amd64.zip` | 仅命令行运行时（不含 IDE） | [下载 CLI](https://github.com/gatesenman/aether/releases/download/v0.6.0/aether-0.6.0-windows-amd64.zip) |

SHA256：

```
4e0e682c8fa430db50688d1c55e2a6c05b15d865254db656b0ca7326aa47bc86  Aether-Setup-0.6.0-win32-x64.exe
71c6bb926e08491cbe6cb1dcea0faa59c485428c4655d163060e25ed7f7d8e6e  Aether-IDE-0.6.0-win32-x64.zip
6f7dbc7ee58b7e6c6bf8d75888f8f9d173a1681e49034e7bd4e65d00109d037c  aether-0.6.0-windows-amd64.zip
```

安装步骤、默认目录、静默参数与排障见 [Windows 安装说明](docs/windows-install.md)。安装包未做代码签名，Windows SmartScreen 可能提示「未知发布者」，可在「更多信息」后选择「仍要运行」。

本轮核验：Setup 为 Windows PE + NSIS（`MZ` / `NullsoftInst`）；便携包内含 `Aether IDE.exe` 与 `bin/aether.exe`；在 Linux 上用 Wine 运行 `aether.exe --version` 得到 `aether 0.6.0 (native)`，哈希与构建目录 `SHA256SUMS` 一致。安装包**尚未在 Windows 真机上安装或启动 IDE**，不能视为已完成 Windows 烟测。

产品主界面的清晰截图目前缺失，待补；不使用有缺字或非真实界面的图片充数。

## 功能特性

- 一份 `.ae` 源码覆盖类型、泛型、闭包、代数数据类型、模式匹配、异常、并发与效果系统。
- 面向智能体的工具链：JSON IR、结构化诊断、结构化补丁协议，以及 MCP 接入（`aether mcp`）和 `aether agents -w` 生成 AGENTS.md。
- Web IDE「Aether Studio」与 VS Code 扩展，支持人机协作编辑。
- 0.6.0 新增：57 个错误码中文说明（`aether explain` / `aether errors`）、`aether migrate` 数据迁移、OpenAPI 3.1 导出（`aether openapi`）。
- 可选原生 Go 运行时（`native/`），不必依赖浏览器。

## 技术栈与平台

- 语言与 Python 工具链（需 Python 3.9+）
- 原生运行时：Go 1.26+
- 编辑器：VS Code 扩展与 Web IDE；官方 Windows 安装包是基于预编译 VSCodium 1.135 改造的 Aether IDE（win32-x64）
- 当前提供的预编译安装包 / 便携包面向 **Windows x64**。Linux / macOS 可从源码构建（见下方与 `ide/build-ide.sh`）。

## 限制

- Aether IDE 外壳使用预编译 VSCodium（Code - OSS，MIT），不是从 VS Code 源码完整编译。
- Windows 可执行文件目前未代码签名；内嵌图标仍可能显示为 VSCodium 原图标。
- Python 包是参考实现；日常 Windows IDE 使用的是原生 `aether.exe`。
- 仓库根目录未附带 LICENSE 文件；Python 包在 `pyproject.toml` 中声明为 MIT。

## 从源码运行

源码位于仓库根目录。预编译包在 [Releases](https://github.com/gatesenman/aether/releases/tag/v0.6.0)。Windows 图形安装向导源码在 [`installer/nsis/`](installer/nsis/)，说明见 [源码说明.md](源码说明.md) 与 [Windows 安装说明](docs/windows-install.md)。

```bash
# Linux / macOS
./install.sh
# Windows
install.bat

aether new 我的项目 --template todo
aether doctor
cd 我的项目
aether serve        # 或 aether studio / aether ui
```
也可用可编辑安装：`pip install -e .`（见 `pyproject.toml`）。原生运行时见 `native/`。

## 文档

- [docs/evolution-0.6.md](docs/evolution-0.6.md)
- [docs/native-runtime.md](docs/native-runtime.md)
- [docs/ide-lsp.md](docs/ide-lsp.md)
- [docs/review-v0.5.md](docs/review-v0.5.md)
- [docs/mvc.md](docs/mvc.md)
- [更新日志 0.6.0](changelog-0.6.0.md)
- [0.6 自进化记录](evolution-0.6.md)
- [语言规范](docs/02-语言规范.md)
- [架构](docs/03-架构.md)
- [IDE 说明](ide/README-IDE.md)
- [Windows 安装说明](docs/windows-install.md)
- [源码说明](源码说明.md)

## 贡献

欢迎通过 Issue 和 Pull Request 参与，详见 [CONTRIBUTING.md](CONTRIBUTING.md)。安全问题请按 [SECURITY.md](SECURITY.md) 私下报告。参与时请遵守 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。

## 许可证

Python 包在 `pyproject.toml` 中声明为 MIT。仓库根目录未附带 LICENSE 文件。编辑器资源还包含第三方字体/图标许可证。

---

## English

# Aether Language

**0.6.0** · A programming language, runtime, and IDE shared by humans and agents: one `.ae` file describes the database, backend API, and UI together.

## Download (Windows x64, 0.6.0)

Release page: [GitHub Releases v0.6.0](https://github.com/gatesenman/aether/releases/tag/v0.6.0)

| Package | Purpose | Link |
|---|---|---|
| `Aether-Setup-0.6.0-win32-x64.exe` | Official NSIS graphical setup (recommended) | [Download Setup](https://github.com/gatesenman/aether/releases/download/v0.6.0/Aether-Setup-0.6.0-win32-x64.exe) |
| `Aether-IDE-0.6.0-win32-x64.zip` | Portable build: run `Aether IDE.exe` after unzip, or use `install.bat` | [Download portable](https://github.com/gatesenman/aether/releases/download/v0.6.0/Aether-IDE-0.6.0-win32-x64.zip) |
| `aether-0.6.0-windows-amd64.zip` | CLI runtime only (no IDE) | [Download CLI](https://github.com/gatesenman/aether/releases/download/v0.6.0/aether-0.6.0-windows-amd64.zip) |

SHA256 values are listed in the Chinese section above. Setup is unsigned; Windows SmartScreen may warn about an unknown publisher. Full install steps: [Windows install notes](docs/windows-install.md) (Chinese).

Checks in this pass: the Setup file is a Windows PE / NSIS installer; the portable zip contains `Aether IDE.exe` and `bin/aether.exe`; `aether.exe --version` under Wine on Linux printed `aether 0.6.0 (native)`; hashes match the build manifest. The packages have **not** been installed or launched on a real Windows machine in this pass, so they are not claimed as Windows smoke-tested.

Clear product UI screenshots are not available yet and are left as a follow-up. Broken or invented screenshots are not used.

## Features

- A `.ae` source file covers types, generics, closures, algebraic data types, pattern matching, exceptions, concurrency, and an effect system.
- Agent-oriented toolchain: JSON IR, structured diagnostics, a structured patch protocol, MCP access (`aether mcp`), and `aether agents -w` to generate AGENTS.md.
- Aether Studio (web IDE) and a VS Code extension for human–agent collaboration.
- 0.6.0 adds Chinese explanations for 57 error codes (`aether explain` / `aether errors`), `aether migrate`, and OpenAPI 3.1 export (`aether openapi`).
- Optional native Go runtime under `native/`, without requiring a browser.

## Tech stack and platforms

- Language and Python toolchain (Python 3.9+)
- Native runtime: Go 1.26+
- Editors: VS Code extension and web IDE; the official Windows package is Aether IDE, a rebranded prebuilt VSCodium 1.135 (win32-x64)
- Prebuilt installer / portable packages currently target **Windows x64**. Linux / macOS can be built from source (see below and `ide/build-ide.sh`).

## Limitations

- The Aether IDE shell uses prebuilt VSCodium (Code - OSS, MIT); it is not a full build from VS Code source.
- Windows executables are not code-signed; the embedded icon may still be the original VSCodium icon.
- The Python package is a reference implementation; the Windows IDE uses the native `aether.exe`.
- There is no LICENSE file at the repository root. The Python package declares MIT in `pyproject.toml`.

## Getting started from source

Source is at the repository root. Prebuilt packages are on the [v0.6.0 release](https://github.com/gatesenman/aether/releases/tag/v0.6.0). The Windows setup wizard source is in [`installer/nsis/`](installer/nsis/); see [源码说明.md](源码说明.md) and [Windows 安装说明](docs/windows-install.md).

```bash
# Linux / macOS
./install.sh
# Windows
install.bat

aether new my-project --template todo
aether doctor
cd my-project
aether serve        # or aether studio / aether ui
```
Alternatively: `pip install -e .` (see `pyproject.toml`). The native runtime is under `native/`.

## Documentation

- [docs/evolution-0.6.md](docs/evolution-0.6.md)
- [docs/native-runtime.md](docs/native-runtime.md)
- [docs/ide-lsp.md](docs/ide-lsp.md)
- [docs/review-v0.5.md](docs/review-v0.5.md)
- [docs/mvc.md](docs/mvc.md)
- [Changelog 0.6.0](changelog-0.6.0.md)
- [0.6 evolution notes](evolution-0.6.md)
- [Language spec](docs/02-语言规范.md)
- [Architecture](docs/03-架构.md)
- [IDE notes](ide/README-IDE.md)
- [Windows install notes](docs/windows-install.md)
- [Source tree notes](源码说明.md)

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md). Please report vulnerabilities privately via [SECURITY.md](SECURITY.md). This project follows [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## License

The Python package declares MIT in `pyproject.toml`. There is no LICENSE file at the repository root. Editor assets also include third-party font/icon licenses.
