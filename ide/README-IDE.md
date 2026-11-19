# Aether IDE 0.5.1

基于 **VSCodium 1.135（Code - OSS，MIT 许可）预编译二进制**改造的品牌发行版（未从源码完整构建 VS Code）：

- product.json 改名为 Aether IDE（独立的数据目录 `~/.aether-ide`、URL 协议 `aether-ide://`），扩展市场为 **Open VSX**
- 内置扩展 `aether-lang.aether-vscode`：语言服务器（原生 `aether lsp`）、语法高亮、应用预览、右侧**智能体面板**、语义历史、契约、测试
- 默认主题 **Aether Dark**、右侧辅助栏默认可见、关闭第三方 AI 聊天界面、遥测关闭
- 自带原生运行时 `aether`（`bin/aether`），无需 Python/浏览器

启动：Linux `./bin/aether-ide` 或 `./aether-ide`；Windows 运行 `install.bat`（或直接运行 `Aether IDE.exe`）；macOS 解压后打开 `Aether IDE.app`。

注意：macOS 版本修改了已签名的 VSCodium 包，签名失效，首次打开需在终端执行 `xattr -cr "/Applications/Aether IDE.app"` 或右键选择“打开”。Windows 可执行文件内嵌图标仍为 VSCodium 原图标（未改写 PE 资源）。
