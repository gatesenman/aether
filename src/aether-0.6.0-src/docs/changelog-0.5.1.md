# Aether 0.5.1 更新说明

## 重点

- **Windows 一键安装**：`Aether-Setup-0.5.1-win32-x64.exe`（Go 自解压）+ 完善的 `Install-Aether.ps1`（快捷方式、PATH、卸载、GrokBot-Apps 同步）。
- **`aether doctor`**：环境自检（OS/架构/PATH/可写目录/CRLF+中文路径 Hello/AeStore）。
- **真机稳定性**：智能体面板启动即激活；CRLF `.ae`；中文路径；`serve` 支持 Ctrl+C 优雅退出并落盘压缩。
- **AeStore**：压缩时先关文件再替换（Windows 可改名）；Windows 下以共享读写方式打开；提供 `Close` / `MaybeCompact`。
- **类型提示**：未定义名字也走编辑距离「你是不是想写 …」。

## 相对 0.5.0

| 项 | 0.5.0 | 0.5.1 |
|---|---|---|
| Windows 安装 | 手动解压 + 简陋 install.ps1 | Setup.exe / Install-Aether.ps1 |
| doctor | 无 | 有 |
| CRLF 源码 | 依赖词法跳过 `\r` | `readSrc` 规范化 |
| 存储压缩 | 打开时压缩，Windows 上句柄占用可能导致失败 | 关句柄→替换→重开；写入过长也会压缩 |
| 智能体面板 | 启动 focus 一次 | focus + 可见时重绘 + 失焦后再试 |

## 未改动的限制

- Windows / macOS 仍无原生 GUI 窗口（用 Aether IDE 或 TUI）。
- Setup.exe 未代码签名。
- IDE 本体仍基于 VSCodium 预编译包换皮；0.5.1 刷新内置扩展与 `aether.exe`，不强制重编 Electron。
