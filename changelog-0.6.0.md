# Aether 0.6.0 更新说明

三轮“竞品 → 开源 → 反思”自进化的成果，详见 `evolution-0.6.md`。

- **错误码目录**：57 个错误码中文解释；`aether explain E200`、`aether errors`；check 显示源码行；LSP 显示中文修法。
- **数据迁移**：`aether migrate`（计划 / `--apply` / `--drop`），M400–M404。
- **OpenAPI 3.1**：`aether openapi`、`/api/_openapi31`（带 schema）；`/api/_openapi` 保持兼容。
- **智能体接入**：`aether mcp`（7 个只读工具）、`aether agents -w` 生成 AGENTS.md，`aether new` 自动生成。
- **IDE 扩展**：MCP 服务器注册；命令“解释错误码 / 生成 AGENTS.md / 数据迁移计划”。
- `aether run --db F` 生效。
- **Windows 安装向导**：正式安装包改为 NSIS 3 中文图形向导（源码 `installer/nsis/`）；`SRC`/`OutFile` 使用相对路径，可在编译时覆盖。
