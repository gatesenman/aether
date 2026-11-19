# Aether 语言（0.6.0）

人与智能体共用的编程语言 + 运行时 + Aether IDE。

## 版本位置

- **0.6.0（最新）**：`安装包\0.6.0\`；源码在 `源代码\aether-0.6.0-src.tar.gz`
- 0.5.1：`安装包\0.5.1\`；源码 `源代码\aether-0.5.1-src.tar.gz`
- 0.5.0：`安装包\Aether-IDE-0.5.0-win32-x64.zip`；源码 `源代码\aether-0.5.0-src.tar.gz`

## 0.6.0 文件说明

| 文件 | 说明 |
|---|---|
| `Aether-Setup-0.6.0-win32-x64.exe` | **标准图形安装向导**（NSIS，中文；欢迎→许可→组件→安装位置→进度条→完成。按当前用户安装到 `%LOCALAPPDATA%\Programs\Aether IDE`，无需管理员；自动创建开始菜单/桌面快捷方式、加入 PATH、在「已安装的应用」可卸载。约 158 MB，SHA256 `4e0e682c…47bc86` 已校验） |
| `aether-0.6.0-windows-amd64.zip` | 仅命令行运行时 |
| `Install-Aether.ps1` / `install.bat` | 脚本安装 |
| `Aether-Launcher.exe` | 启动器 |
| `SHA256SUMS` | 校验和 |
| `evolution-0.6.md` / `changelog-0.6.0.md` | 三轮自进化报告 / 更新日志 |

## 0.6.0 新增

- `aether explain E200`、`aether errors`：57 个错误码中文说明
- `aether migrate`（先计划，`--apply` 执行）、`aether openapi`（OpenAPI 3.1）
- `aether mcp`（智能体接入）、`aether agents -w`（生成 AGENTS.md）

## 快速开始

1. 双击 **`安装包\0.6.0\Aether-Setup-0.6.0-win32-x64.exe`**，按向导点「下一步」即可（如弹出 SmartScreen：更多信息 → 仍要运行）。卸载：设置 → 应用 → 已安装的应用 → Aether IDE 0.6.0。
2. 打开 **Aether IDE**，或在终端：

```bat
aether new 我的项目 --template todo
aether doctor
cd 我的项目
aether ui main.ae
```

## 卸载

```bat
powershell -ExecutionPolicy Bypass -File "%LOCALAPPDATA%\Aether\Uninstall-Aether.ps1"
```