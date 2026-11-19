<p align="center"><img src="src/aether-0.6.0-src/editors/vscode/media/aether.png" alt="Aether Language" width="128"></p>

# Aether 语言

**0.6.0** · 人与智能体共用的编程语言、运行时与 Aether IDE：一份 `.ae` 文件同时描述数据库、后端 API 和前端界面。

## 功能特性

- 一份 `.ae` 源码覆盖类型、泛型、闭包、代数数据类型、模式匹配、异常、并发与效果系统。
- 面向智能体的工具链：JSON IR、结构化诊断、结构化补丁协议，以及 MCP 接入（`aether mcp`）和 `aether agents -w` 生成 AGENTS.md。
- Web IDE「Aether Studio」与 VS Code 扩展，支持人机协作编辑。
- 0.6.0 新增：57 个错误码中文说明（`aether explain` / `aether errors`）、`aether migrate` 数据迁移、OpenAPI 3.1 导出（`aether openapi`）。
- 可选原生 Go 运行时（`native/`），不必依赖浏览器。

## 技术栈

- 语言与 Python 工具链（需 Python 3.9+）
- 原生运行时：Go 1.26+
- 编辑器：VS Code 扩展与 Web IDE

## 从源码运行

源码位于 `src/aether-0.6.0-src/`。本仓库不包含安装包。

```bash
cd src/aether-0.6.0-src
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

- [更新日志 0.6.0](changelog-0.6.0.md)
- [0.6 自进化记录](evolution-0.6.md)
- [源码说明](src/aether-0.6.0-src/README.md)
- [语言规范](src/aether-0.6.0-src/docs/02-语言规范.md)
- [架构](src/aether-0.6.0-src/docs/03-架构.md)
- [IDE 说明](src/aether-0.6.0-src/ide/README-IDE.md)
- [Windows 安装说明](src/aether-0.6.0-src/docs/windows-install.md)

## 许可证

Python 包在 `src/aether-0.6.0-src/pyproject.toml` 中声明为 MIT。仓库根目录未附带 LICENSE 文件。编辑器资源还包含第三方字体/图标许可证。

---

## English

# Aether Language

**0.6.0** · A programming language, runtime, and IDE shared by humans and agents: one `.ae` file describes the database, backend API, and UI together.

## Features

- A `.ae` source file covers types, generics, closures, algebraic data types, pattern matching, exceptions, concurrency, and an effect system.
- Agent-oriented toolchain: JSON IR, structured diagnostics, a structured patch protocol, MCP access (`aether mcp`), and `aether agents -w` to generate AGENTS.md.
- Aether Studio (web IDE) and a VS Code extension for human–agent collaboration.
- 0.6.0 adds Chinese explanations for 57 error codes (`aether explain` / `aether errors`), `aether migrate`, and OpenAPI 3.1 export (`aether openapi`).
- Optional native Go runtime under `native/`, without requiring a browser.

## Tech stack

- 语言与 Python 工具链（需 Python 3.9+）
- 原生运行时：Go 1.26+
- 编辑器：VS Code 扩展与 Web IDE

## Getting started

Source lives in `src/aether-0.6.0-src/`. This repository does not include installers.

```bash
cd src/aether-0.6.0-src
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

- [更新日志 0.6.0](changelog-0.6.0.md)
- [0.6 自进化记录](evolution-0.6.md)
- [源码说明](src/aether-0.6.0-src/README.md)
- [语言规范](src/aether-0.6.0-src/docs/02-语言规范.md)
- [架构](src/aether-0.6.0-src/docs/03-架构.md)
- [IDE 说明](src/aether-0.6.0-src/ide/README-IDE.md)
- [Windows 安装说明](src/aether-0.6.0-src/docs/windows-install.md)

## License

The Python package declares MIT in `src/aether-0.6.0-src/pyproject.toml`. There is no LICENSE file at the repository root. Editor assets also include third-party font/icon licenses.

