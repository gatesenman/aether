# Aether 语言支持（VS Code / Cursor / VSCodium / Aether IDE）

把 Aether 的原生语言服务器 `aether lsp` 接入编辑器：

- 诊断（类型、名称、效果、契约、视图引用）、悬停、补全、转到定义、查找引用、重命名、大纲、格式化、语义着色
- 代码操作：检查器提示的快速修复（如 did you mean），以及**智能体补丁**（结构化补丁，先类型检查）
- 应用预览（同一份 .ae 渲染出的界面 + API + 数据，实时刷新）
- 智能体面板、语义历史（撤销/重做）、契约视图、测试资源管理器、运行程序/流程
- 平台专用 .vsix 内置原生二进制；通用 .vsix 使用 PATH 中的 `aether` 或设置 `aether.server.path`

图标全部来自 Codicons 字体（CC-BY-4.0），不使用 emoji。
