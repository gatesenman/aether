# Aether 中的 Model / View / Controller（v0.5）

很多人第一次打开 Aether 只看到“页面”，会以为模型和控制器“没有”。其实三层都在同一个 `.ae` 文件里，
只是**不需要写胶水代码**：数据库表、REST 接口、表单校验都由声明自动生成。v0.5 把三层在 IDE 里显式展示出来。

| 层 | Aether 语法 | 自动得到 | IDE 里在哪看 |
|---|---|---|---|
| **Model（模型 / 数据）** | `entity`（字段、默认值、`invariant` 不变式、`*_id` 关系） | 数据表（AeStore，WAL 持久化）、`GET/POST /api/<实体>`、`GET/PUT/DELETE /api/<实体>/<id>`、写入时校验不变式 | 侧栏「模型」树；「架构 → 模型 · ER 图」；「数据浏览」查看/编辑记录 |
| **Controller（控制 / 业务逻辑）** | `action`（`requires`/`ensures` 契约、`allow` 角色）、`flow`（多步骤、`retry`/`backoff`、可恢复）、`auth { can … }` 权限、`fn` | `POST /api/action/<名>`、`POST /api/flow/<名>`、`/api/_flows/runs`（运行记录与恢复）、登录 `/api/_auth/*`、403/401 权限检查 | 侧栏「控制器」树；「架构 → 控制器」：动作表、流程图、权限矩阵、全部接口；「接口测试」 |
| **View（界面）** | `view`：`title/section/form/list/table/chart/stat/button`，布局 `row/column/grid/card`，`style {..}`，`theme {..}` | 网页界面（含登录框），表单 → 实体接口，按钮 → 动作接口 | 侧栏「界面」树；「预览」；「架构 → 界面」 |

## 示例：`examples/v05/crm_mvc.ae`（模板 `aether new 项目 --template crm`）

下面是压缩成几行的示意（省略了部分内容，完整可运行代码见该文件）：

```aether
// ===== Model =====
entity Customer { name: Text  phone: Text = ""  level: Text = "普通"  invariant len(name) > 0 }
entity Deal { title: Text  customer_id: Int  amount: Int = 0  stage: Text = "线索" }   // customer_id → 关系 Deal→Customer

// ===== Controller =====
auth { roles admin, sales  default sales  can sales write Deal  can sales read Customer }
action advance(d: Deal) allow sales requires d.stage != "成交" { … }
flow 每周报告 { step "统计" retry 2 backoff 100ms { … }  step "生成报告" { … } }

// ===== View =====
view 总览 { title "客户管理"  row { card "客户" { stat "客户数" count(Customer) } }  form Customer  table Customer }
```

命令行同样可以看到三层：

```
$ aether mvc examples/v05/crm_mvc.ae
== Model（模型：数据） ==
  entity Customer { name: Text, phone: Text, level: Text }
      invariant len(name) > 0
  entity Deal { title: Text, customer_id: Int → Customer, amount: Int, stage: Text }
      invariant amount >= 0
== Controller（控制：动作、流程、权限） ==
  action advance  POST /api/action/advance  allow=[sales]  touches=[Deal]
  action upgrade  POST /api/action/upgrade  allow=[admin]  touches=[Customer]
  flow 每周报告: 统计 → 生成报告
  auth roles=[admin sales] login=required signup=open
== View（界面） ==
  view 总览  uses=[Customer]  buttons=[]
  view 商机  uses=[Deal]  buttons=[]
```

`aether mvc --json` / LSP 请求 `aether/model` 返回同样的结构，供 IDE 和智能体使用。

## 约定优于配置（v0.5）

- 没有写任何 `view` 时，每个实体自动得到一个“表单 + 列表”页面；每个实体总是自动有 REST 接口。
- 字段类型可省略：`done = false` → `Bool`，`rank = 1` → `Int`，`title` → `Text`，`owner_id` → `Int`，`is_x`/`has_x` → `Bool`。
- 一行 `entity Note { title, body, pinned = false }` 就是一个可运行、可持久化、带接口和界面的应用。

截图：`screenshots/v05_mvc_mapping.png`、`v05_mvc_er_diagram.png`、`v05_mvc_controller.png`、`v05_data_browser.png`、`v05_api_tester.png`。
