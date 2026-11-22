# Windows 安装说明（Aether 0.6.0）

## 一键安装（推荐）：标准图形安装向导

`Aether-Setup-0.6.0-win32-x64.exe` 用 **NSIS 3 + Modern UI 2** 制作，是标准的 Windows 图形安装向导，不会弹出黑色控制台窗口。界面为简体中文，安装包用 LZMA 固实压缩。本仓库只包含安装器源码，不含预编译的 Setup.exe。

1. 双击 `Aether-Setup-0.6.0-win32-x64.exe`。若 SmartScreen 提示「已保护你的电脑」，点「更多信息」→「仍要运行」（安装包未做代码签名）。
2. 向导依次为：**欢迎 → 许可与说明 → 选择组件 → 选择安装位置 → 安装进度条 → 完成**（可勾选「运行 Aether IDE」）。
3. 组件：
   - Aether IDE（必需）
   - 桌面快捷方式（默认勾选）
   - 将 aether 命令加入 PATH（默认勾选，只改当前用户 PATH）
4. 默认按**当前用户**安装到 `%LOCALAPPDATA%\Programs\Aether IDE`，**不需要管理员权限，也不弹 UAC**。
5. 安装完成后：
   - 开始菜单 `Aether IDE` 文件夹里有「Aether IDE」和「卸载 Aether IDE」
   - 桌面有 Aether IDE 快捷方式（Aether 图标）
   - `设置 → 应用 → 已安装的应用`（控制面板「程序和功能」）里能看到 **Aether IDE 0.6.0**（发布者 Aether Project）
   - 命令行工具位于 `%LOCALAPPDATA%\Programs\Aether IDE\bin`

新开一个终端验证：

```bat
aether version
aether doctor
```

### 卸载

在「已安装的应用」里卸载，或运行 `安装目录\Uninstall.exe`。卸载会删除程序文件、开始菜单和桌面快捷方式，并从用户 PATH 中移除 `bin` 目录，其他 PATH 项保持不变。静默卸载用 `Uninstall.exe /S`。

### 静默安装（适合脚本/智能体）

```bat
Aether-Setup-0.6.0-win32-x64.exe /S
Aether-Setup-0.6.0-win32-x64.exe /S /D=D:\Apps\Aether IDE
```

`/D=` 必须是最后一个参数，路径不要加引号。

### 从源码构建安装包

需要已安装 NSIS（`makensis`），以及事先构建/解压得到的 `aether-ide-win32-x64` 目录（见 `ide/build-ide.sh` 与 `源码说明.md`）。`installer/nsis/aether.nsi` 中的 `SRC` 与 `OutFile` 已写成相对路径（相对于该 `.nsi` 文件，默认指向 `../../release-0.6.0/`）。可按本机布局覆盖：

```bat
makensis -DSRC=C:\path\to\aether-ide-win32-x64 -DOUTFILE=C:\path\to\Aether-Setup-0.6.0-win32-x64.exe installer\nsis\aether.nsi
```

Windows 上也可以写成 `/DSRC=...` `/DOUTFILE=...`。

## 没有 Setup.exe 时（脚本安装，旧布局）

把发布包里的下列文件放在同一文件夹：

- `Aether-IDE-0.6.0-win32-x64.zip`（或已解压的 `aether-ide-win32-x64\`）
- `Install-Aether.ps1` + `install.bat`

然后：

```bat
install.bat
```

或：

```bat
powershell -NoProfile -ExecutionPolicy Bypass -File Install-Aether.ps1
```

可选参数：

```bat
powershell -ExecutionPolicy Bypass -File Install-Aether.ps1 -Dest "D:\Tools\Aether"
powershell -ExecutionPolicy Bypass -File Install-Aether.ps1 -ZipPath ".\Aether-IDE-0.6.0-win32-x64.zip"
```

仅需命令行运行时（不要 IDE）时，解压 `aether-0.6.0-windows-amd64.zip`，运行其中的 `install.ps1`。

此方式默认安装到 `%LOCALAPPDATA%\Aether\`（与 NSIS 向导的 `%LOCALAPPDATA%\Programs\Aether IDE` 不同）。卸载：

```bat
powershell -ExecutionPolicy Bypass -File "%LOCALAPPDATA%\Aether\Uninstall-Aether.ps1"
```

会删除该安装目录、桌面/开始菜单快捷方式，并从用户 PATH 移除 `bin`。

## 故障排除

| 现象 | 处理 |
|---|---|
| SmartScreen /「未知发布者」 | 「仍要运行」。企业环境可让管理员加信任或自行签名。 |
| 双击 Setup 无反应 | 在 PowerShell 中运行：`& '.\Aether-Setup-0.6.0-win32-x64.exe'`，查看报错。 |
| `aether` 不是内部或外部命令 | 关掉所有终端再开；检查用户 Path 是否包含 NSIS 默认的 `%LOCALAPPDATA%\Programs\Aether IDE\bin`，或脚本安装的 `%LOCALAPPDATA%\Aether\bin`。 |
| IDE 智能体面板空白 | 命令面板运行「View: Focus on 智能体 View」，或重装 0.6.0（已在启动时强制激活 webview）。 |
| Restricted Mode / 限制模式 | Aether IDE 默认信任工作区；若仍受限，右下角点「管理」→「信任」。 |
| `.ae` 文件在记事本保存后检查失败 | 0.6.0 已自动兼容 CRLF；请确认文件编码为 UTF-8。 |
| 路径含中文 | 已支持。若仍失败，运行 `aether doctor` 把输出发给我们。 |
| Ctrl+C 停不掉 `aether serve` | 0.6.0 会优雅退出并压缩存储；若卡住，再按一次或关窗口。 |
| 杀毒软件隔离 `aether.exe` | 添加到白名单；二进制为 Go 静态编译、无安装服务。 |

## 安装器实现说明（诚实）

- **`Aether-Setup-0.6.0-win32-x64.exe`**：NSIS 3 + Modern UI 2 图形安装向导（源码在 `installer/nsis/`）。把已构建的 `aether-ide-win32-x64` 目录打进安装包，写入开始菜单、桌面快捷方式、卸载程序和当前用户 PATH。安装包本身没有代码签名。
- **`Install-Aether.ps1` / `install.bat`**：脚本安装备选方案，可在没有 Setup.exe 时单独使用，默认目录与 NSIS 向导不同。
- 仓库里仍保留更早的 Go 自解压器源码（`installer/sfx/`、`installer/pack_setup.go`）和可选启动器（`installer/launcher/`），**当前推荐的正式安装包是 NSIS 向导**，不是自解压黑窗口程序。

## 与本机交付目录

若本机曾用交付目录（例如 `GrokBot-Apps\Aether语言\`）存放安装包，请以该目录下 `安装包\0.6.0\` 中的 `Aether-Setup-0.6.0-win32-x64.exe` 为准；与仓库源码版本应对应 0.6.0。
