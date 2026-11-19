# Windows 安装说明（Aether 0.5.1）

## 一键安装（推荐）

1. 下载 **`Aether-Setup-0.5.1-win32-x64.exe`**（自解压安装器：内含 IDE + 运行时 + `Install-Aether.ps1`）。
2. 双击运行。若 SmartScreen 提示「已保护你的电脑」，点「更多信息」→「仍要运行」（当前安装包未做 EV 代码签名）。
3. 安装完成后：
   - 桌面与开始菜单会出现 **Aether IDE** 快捷方式
   - 命令行工具 `aether.exe` 已加入**用户 PATH**（`%LOCALAPPDATA%\Aether\bin`）
   - 默认安装目录：`%LOCALAPPDATA%\Aether\`
     - `IDE\` — 完整 IDE
     - `bin\aether.exe` — 语言运行时 / LSP / 调试器
     - `Uninstall-Aether.ps1` — 卸载脚本

新开一个终端，验证：

```bat
aether version
aether doctor
```

`aether doctor` 会打印操作系统、架构、PATH、可写目录，并在含中文的临时路径下跑一遍「Hello」检查与存储引擎自检。

## 没有 Setup.exe 时

把发布包里的下列文件放在同一文件夹：

- `Aether-IDE-0.5.1-win32-x64.zip`（或已解压的 `aether-ide-win32-x64\`）
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
powershell -ExecutionPolicy Bypass -File Install-Aether.ps1 -ZipPath ".\Aether-IDE-0.5.1-win32-x64.zip"
```

仅需命令行运行时（不要 IDE）时，解压 `aether-0.5.1-windows-amd64.zip`，运行其中的 `install.ps1`。

## 卸载

```bat
powershell -ExecutionPolicy Bypass -File "%LOCALAPPDATA%\Aether\Uninstall-Aether.ps1"
```

会删除安装目录、桌面/开始菜单快捷方式，并从用户 PATH 移除 `bin`。

## 故障排除

| 现象 | 处理 |
|---|---|
| SmartScreen /「未知发布者」 | 「仍要运行」。企业环境可让管理员加信任或自行签名。 |
| 双击 Setup 无反应 | 用管理员 PowerShell 运行：`& '.\Aether-Setup-0.5.1-win32-x64.exe'`，看报错。 |
| `aether` 不是内部或外部命令 | 关掉所有终端再开；或检查「环境变量」用户 Path 是否含 `%LOCALAPPDATA%\Aether\bin`。 |
| IDE 智能体面板空白 | 命令面板运行「View: Focus on 智能体 View」，或重装 0.5.1（已在启动时强制激活 webview）。 |
| Restricted Mode / 限制模式 | Aether IDE 默认信任工作区；若仍受限，右下角点「管理」→「信任」。 |
| `.ae` 文件在记事本保存后检查失败 | 0.5.1 已自动兼容 CRLF；请确认文件编码为 UTF-8。 |
| 路径含中文 | 已支持。若仍失败，运行 `aether doctor` 把输出发给我们。 |
| Ctrl+C 停不掉 `aether serve` | 0.5.1 会优雅退出并压缩存储；若卡住，再按一次或关窗口。 |
| 杀毒软件隔离 `aether.exe` | 添加到白名单；二进制为 Go 静态编译、无安装服务。 |

## 安装器实现说明（诚实）

- **`Aether-Setup-0.5.1-win32-x64.exe`**：自研 Go 自解压程序（SFX）。把 IDE zip + 安装脚本打包进单个 exe；运行时解压到临时目录并调用 `Install-Aether.ps1`。**不是** Inno Setup / NSIS。
- **`Install-Aether.ps1`**：实际完成复制、快捷方式、PATH、卸载脚本；可单独使用。
- **`Aether.exe`（可选启动器）**：安装后位于 `%LOCALAPPDATA%\Aether\`，用于启动 IDE。

## 与 GrokBot-Apps 目录

若本机存在 `C:\Users\Administrator\GrokBot-Apps\Aether语言\`，安装器会额外把 `aether.exe` 同步到其下的 `运行时\`，并写一份 `安装说明-0.5.1.txt`。
