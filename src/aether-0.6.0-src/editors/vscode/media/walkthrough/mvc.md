# Model / Controller / View

| 层 | 写法 | 运行时得到 |
|---|---|---|
| **Model 模型** | `entity Customer { name: Text }` | 数据表、字段校验、增删改查接口 |
| **Controller 控制** | `action` / `flow` / `auth` | 业务接口、权限检查、可恢复流程 |
| **View 界面** | `view` / `theme` | 页面、表单、列表、布局 |

左侧 Aether 图标下有三个浏览器：**模型**、**控制**、**界面**。「架构视图」里能看到 ER 图、权限矩阵、流程图和全部接口。
