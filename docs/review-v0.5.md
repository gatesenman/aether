# Aether v0.5 交叉评审（五个角色 · 独立评审 → 统一排序 → 实施 → 实测）

> 方法：五个角色**分别单独**做一轮评审。每轮只读代码、示例和 v0.4 发布包，不看其他角色的结论，
> 所以各轮的关注点、措辞和打分都不一样。最后一节把五份意见合并、去重，排出优先级，
> 并逐条标注 v0.5 **已实现 / 部分实现 / 推迟**。所有数字都由脚本实测（`bench/run.py`、`tests/`），没有手工修改。

---

## 第一轮 · 语言设计者

**总体判断：** 语义模型（实体、契约、效果、流程、视图同在一份 AST 中）很有特色；但作为“语言”，表达力和静态保证仍是玩具级。

| 问题 | 对比主流 | 严重度 |
|---|---|---|
| 字段访问不做类型检查：`a.balanse` 要到运行时才会出错 | TS/Rust/Go 都在编译期报错 | **致命** |
| 没有模块与命名空间：只能扁平 `import`，没有私有成员 | TS `import {x} from`、Python 包、Go package | 高 |
| 没有包管理与锁文件 | npm/pip/cargo/go mod | 高 |
| 视图只有固定几种组件，不能布局、不能设样式 | React/CSS 任意布局 | 中 |
| 每个字段都必须写类型：样板代码多，token 也多 | Python/TS 的类型推断、Rails 的约定 | 中 |
| 没有泛型约束、trait/接口、模式匹配穷尽性检查 | Rust/TS | 低（推迟） |

**特性对照矩阵（语言层面）**

| 能力 | Aether v0.4 | TypeScript | Python | Rust | Go | SQL | React |
|---|---|---|---|---|---|---|---|
| 静态类型 | 部分（函数签名） | ✔ | 可选 | ✔ | ✔ | 表结构 | 借助 TS |
| 字段级类型检查 | ✘ | ✔ | mypy | ✔ | ✔ | ✔ | 借助 TS |
| 类型推断 | 局部 | ✔ | — | ✔ | `:=` | — | — |
| 模块/命名空间 | 扁平 | ✔ | ✔ | ✔ | ✔ | schema | ✔ |
| 包管理 | ✘ | npm | pip | cargo | go mod | — | npm |
| 契约（前置/后置/不变式） | **✔ 原生** | ✘ | ✘ | ✘ | ✘ | CHECK | ✘ |
| 效果标注 | **✔** | ✘ | ✘ | 部分 | ✘ | — | ✘ |
| 持久化数据模型 | **✔ 原生** | ORM | ORM | ORM | ORM | ✔ | ✘ |
| 布局/样式 | ✘ | — | — | — | — | — | ✔ |

**这一轮的建议：** 先补字段类型检查（并给出“你是不是想写…”的提示），再做模块、别名和私有成员，然后是类型推断。

---

## 第二轮 · 编译器 / 运行时工程师

**总体判断：** Go 原生运行时不依赖 CGO、单文件分发，这一点做得好。但“能运行”和“能在生产上运行”之间还差持久性、可恢复性、调试和安全。

1. **流程不持久**：`flow` 运行到一半进程退出，状态就全丢了；也没有重试和退避。对比 Temporal、Airflow、AWS Step Functions，这是根本缺陷。
2. **没有调试器**：只能 `print`。主流语言都通过 DAP 支持断点、单步、变量查看。
3. **权限是假的**：角色由请求头 `X-Role` 决定，任何人都能把自己改成 admin。没有用户、口令和令牌。
4. **部署靠手工**：没有可复现的部署包（Dockerfile、systemd、启动脚本）。
5. **包没有完整性校验**：没有锁文件，也不校验哈希。
6. 次要：AST 哈希要保持稳定，新语法不能悄悄改变旧程序的哈希；一致性套件（72 个用例）必须继续全部通过。

**这一轮的建议：** 依次做流程检查点、DAP、真实认证（PBKDF2 + 签名令牌）、`deploy`，以及 `aether.lock` 的 sha256 校验。

---

## 第三轮 · IDE / 交互设计师

**总体判断：** v0.4 的 IDE 已经很像 VS Code，但新手第一次打开时不知道从哪里开始。另外 Model 和 Controller 层“看不见”，用户以为只有页面。

| 问题 | 现象 | 对比 |
|---|---|---|
| 智能体面板第一次打开是空白 | 必须切换一次视图才显示 | Copilot Chat、Cursor 打开即可用 |
| 没有“新建项目”向导和入门教程 | 新手面对空白编辑器 | VS Code 的 walkthrough、create-react-app |
| 只看得到 View | 实体、动作、权限、接口没有可视化 | Prisma Studio、Django Admin、Swagger UI、Rails ER 图 |
| 没有数据浏览器、接口测试器 | 只能在预览里点 | Postman、TablePlus |
| 没有调试 UI | — | 所有主流 IDE |
| Windows 版 exe 还是 VSCodium 的图标 | 品牌不一致 | — |
| 原生 Studio：不能选择、复制、粘贴，鼠标悬停没反应，不能输入中文 | 基本编辑缺失 | 任何编辑器 |

**这一轮的建议：**
- 加 MVC 三棵树、架构页（ER 图、流程图、权限矩阵、接口列表）、数据浏览、接口测试。
- 加向导、中文 walkthrough、F5 调试、一键部署。
- 修复面板空白和 exe 图标。

---

## 第四轮 · 智能体协作架构师

**总体判断：** “结构化补丁 + 类型检查后再接受”是对的方向。但真实的 LLM 路径没有闭环；对智能体来说，信息密度也不够高。

1. **LLM 只调用一次**：出错就失败，没有“把诊断交回模型重写”的自我修复循环。
2. **上下文太大**：智能体每次都要读整个文件。应当提供“只给相关切片”的接口：目标声明、它的依赖、契约、谁在用它，其余部分只给骨架。
3. **描述冗余**：类型、CRUD、接口都要逐个写出来，浪费 token。应当默认推断，并提供一个可以无损往返的紧凑投影（compact）给模型看。
4. **缺少测量**：说 Aether 对智能体友好，就要有 token 数和首次通过率的实测，并和 TS、Python 技术栈对比。
5. 安全方面：智能体只能改 AST，不能执行任意命令。这一点 v0.4 已经做到，要保持。

**这一轮的建议：**
- 实现 `compact` / `expand` / `skeleton` / `context` 和 LSP `aether/context`。
- 加字段类型推断、隐式 CRUD，以及 LLM 自我修复循环（用 mock 的 OpenAI 兼容服务测试）。
- 用 tiktoken 做基准测试。

---

## 第五轮 · 完全不懂编程的新手用户

（实际流程：按 README 安装 → 打开 IDE → 尝试做一个“客户管理”）

- “打开以后不知道点哪里。希望有个按钮叫‘新建项目’，选个模板就能跑起来。”
- “右边的智能体面板第一次是空白的，我以为坏了。”
- “我说‘加一个手机号字段’，它说听不懂。说‘添加字段 phone: Text’才行。”（离线规则智能体的局限，见下面的实测）
- “别人都能看到我的数据吗？怎么设置登录？”
- “页面都长一个样，能不能把两个卡片并排放？能不能换个颜色？”
- “做好了怎么给同事用？”
- “出错的时候想一步步看程序在干什么。”
- “在原生 Studio 里没法用中文输入法打字，也没法复制粘贴。”

---

## 合并后的优先级与 v0.5 实施情况

| 优先级 | 事项 | 来源 | 状态 | 证据 |
|---|---|---|---|---|
| P0 | 字段级类型检查 E234，带“你是不是想写”提示 | 设计者 | ✅ 已实现 | `tests/test_v05.py::test_field_typing` |
| P0 | 真实登录与权限：`auth{}`、PBKDF2、签名令牌、401/403、首个用户为 admin | 运行时、新手 | ✅ | `test_auth_permissions_layout`，`screenshots/v05_app_login.png` |
| P0 | 智能体面板首次打开空白 | IDE | ✅ 修复 | 扩展测试 18/18 |
| P0 | 新建项目向导、5 个模板、中文 walkthrough（6 步）、项目内 README 教程 | IDE、新手 | ✅ | `v05_e2e_01/02/03` |
| P0 | M/V/C 可视化：三棵树、ER 图、控制器（动作、流程图、权限矩阵、接口）、数据浏览、接口测试，以及 `docs/mvc.md`、`aether mvc` | 用户反馈、IDE | ✅ | `v05_mvc_*`、`v05_data_browser`、`v05_api_tester` |
| P1 | 模块与命名空间（`import "x" as m`，`_` 前缀为私有）；包管理 `aether pkg init/add/install --frozen/remove/list`，`aether.lock` sha256 | 设计者、运行时 | ✅ 基础版 | `test_modules_and_packages` |
| P1 | 可持久、可恢复的流程：检查点、`retry N backoff`、`aether flow runs/resume`、重启后标记为 interrupted | 运行时 | ✅ | `test_flow_resume` |
| P1 | DAP 调试器：断点、单步进入/跳过/跳出、变量、求值；IDE 中 F5 启动 | 运行时、IDE | ✅ | `test_dap_debugger`，`v05_e2e_07/08` |
| P1 | 真实 LLM 路径和自我修复循环（最多 3 轮） | 智能体 | ✅（用 mock OpenAI 服务测试） | `test_llm_self_repair_mock_openai`，`bench/` |
| P1 | 布局与样式：`row/column/grid/card`、`style{}`、`theme{}`、`image`、`link` | 设计者、新手 | ✅ | `test_auth_permissions_layout` |
| P1 | 一键运行与部署：`aether deploy`（Dockerfile、systemd、run.sh/.bat、本地启动）；IDE 中 Ctrl+F5 / 部署按钮 | 运行时、新手 | ✅ | `v05_e2e_09_deploy` |
| P1 | 约定优于配置：字段类型推断、隐式 CRUD 与默认页面 | 智能体 | ✅ | `test_inference_and_projections` |
| P1 | `aether compact/expand/skeleton/context`，LSP `aether/context`、`aether/compact` | 智能体 | ✅ | 同上（所有示例往返后 AST 哈希不变） |
| P1 | token 基准和生成评测 | 智能体 | ✅ | 见下节 |
| P2 | Windows 版 exe 图标（pefile 原地替换 RT_ICON，并修正 PE 校验和） | IDE | ✅ | `ide/peicon.py` |
| P2 | 原生 Studio：选择、复制剪切粘贴（X11 CLIPBOARD 跨进程）、鼠标悬停 | IDE、新手 | ✅ | `x11clip_test.go`，`v05_native_studio_*` |
| P2 | 原生 Studio 中文输入 | 新手 | ⚠️ 部分实现 | 见“局限” |
| P3 | 泛型约束、trait、模式匹配穷尽性检查 | 设计者 | ⏸ 推迟 | — |
| P3 | 包注册中心（远程索引、语义化版本求解） | 设计者 | ⏸ 推迟（目前只支持本地目录、tar.gz、git 和 URL） | — |
| P3 | 真实 LLM 的生成质量评测 | 智能体 | ⏸ 推迟（这台机器上没有可用模型） | — |
| P3 | 多用户实时协作编辑、云端托管部署 | — | ⏸ 推迟 | — |

---

## 智能体友好度：实测（`python3 bench/run.py`，结果在 `bench/results.json`）

### 1. 同一个应用，不同写法的 token 数（tiktoken `o200k_base`；`cl100k_base` 结果相近，见 JSON）

| app | Aether 原文 | 去注释 | compact | skeleton | TS+React+Express+SQL | Python FastAPI+SQLAlchemy+Jinja |
|---|---|---|---|---|---|---|
| todo | 281 | 231 | 190 | 104 | 1936 | 1508 |
| notes | 45 | 21 | 12 | 17 | 794 | 798 |
| crm (no other-stack port) | 647 | 455 | 348 | 184 | — | — |

倍数（todo，o200k）：TS 栈 / Aether 原文 ≈ 6.9×，TS 栈 / compact ≈ 10.2×；Python 栈 / Aether 原文 ≈ 5.4×。

说明（请如实看待这些数字）：
- **todo**：四种写法的功能完全相同，包括实体与校验、REST CRUD、toggle、带前置条件的 bump、仅 admin 可用的 clear_done、进度统计、首页和表格页。
  TS 与 Python 版本由我们手写，追求“最少但完整”，见 `bench/apps/todo/`。熟练的工程师用框架脚手架也许能再省 20–30%，但数量级不会变。
- **notes**：只有一个实体的最小 CRUD 应用。Aether 原文中有 24 个 token 是注释。这里的差距主要来自“隐式 CRUD + 默认页面”，而不是语法本身更短。
- **compact** 删除注释、缩进和可推断的字段类型。`expand(compact(x))` 的 AST 哈希与原文完全相同，这一点对所有示例和模板都做了测试。**注释不会保留**。
- **skeleton** 只有签名级信息，给上下文窗口用，不能还原成原程序。
- 这些对比**不**说明 Aether 能替代通用语言：超出 Aether 内置能力的需求（复杂 UI 交互、第三方 SDK）仍然需要写宿主代码。

### 2. 生成评测（首次 check 通过率）

生成评测（built-in offline rule agent (no LLM available on this box)）：首次 check 通过 18/20，其中确实产生修改 11/20

- 这台机器上**没有可用的本地 LLM**（没有 ollama 或其他推理服务），所以用 IDE 内置的离线规则智能体跑了 20 条真实风格的中英文指令。
- “check 通过”只说明结果可以编译。它包括“没听懂、没有修改”的情况（这时原文不变，自然能通过），所以我们同时报告“通过且确实改了”的数量。
- 逐条查看后：“通过且确实改了”的 11 条中，有 1 条**理解错了**（“add a view 报表 with a table of Product”被当成了新增实体 `with`）。真正正确的是 **10/20**。
- 2 条没有通过检查（shop 模板已经有 `stock` 字段，blog 模板已经有 `Comment` 实体）。这两次是类型检查器**正确拦截**了重复定义，补丁没有被应用。这正是“先检查后接受”设计的作用。
- 结论：离线规则智能体只适合演示，真实使用需要配置 LLM（`aether.llm.*` 设置或 `AETHER_LLM_*` 环境变量）。

### 3. LLM 自我修复循环（mock OpenAI 兼容服务）

自我修复（mock OpenAI 服务注入一次字段拼写错误）：LLM 轮数 2，修复后通过 True，第二轮收到诊断 True，系统提示 413 tokens

mock 服务第一轮故意返回一个字段拼写错误（`t.dnoe`）。第二轮请求中带上了检查器的 E234 诊断（包括“你是不是想写 done”），模型修正后通过了检查。这验证的是**机制**，不代表真实模型的修复率。

---

## 已知局限（诚实列出）

- **原生 Studio 中文输入**：自研的 X11 客户端没有实现 XIM/IBus 协议，所以**系统输入法的候选框不会出现**。
  - 已经做到：粘贴中文、通过 keysym 注入的 Unicode 字符（xdotool、部分输入法的直接提交模式）可以正确输入。为此修复了 MappingNotify 的解析和键盘映射重载的竞争（截图 `v05_native_studio_cjk_input.png`）。
  - 需要完整中文输入的用户请使用 Aether IDE（基于 VSCodium，支持系统输入法）。
- 包管理没有中央注册中心和版本求解。
- 认证没有找回密码和 OAuth。令牌密钥保存在应用数据库的 `_Secret` 表里。
- 部署目标只有本地运行和生成部署包（Docker、systemd）。不会自动推送到云平台，因为那属于对外操作，需要用户自己执行。
- Windows 版 exe 修改图标后原有的 Authenticode 签名会失效。原本就未签名的分发不受影响，但 SmartScreen 依然会提示。
- 本节的生成评测没有使用真实 LLM。
