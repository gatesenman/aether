# Aether 本地十轮深度迭代

基点：10dc771；分支 iteration10-local；不推远端、不操作用户电脑、不提升版本号。

## 基线
- 原工作树 /workspace/aether-github 干净；新建独立 worktree /workspace/aether-iteration10。
- 已阅读 README、0.6 三轮演进记录及解析器、存储、权限、MCP、LSP、项目生成代码。
- 初次 Go 基线因共享根盘无空间失败；改用独立 /dev/shm 临时及缓存目录。该盘 noexec，tools/run-test-binary.sh 只复制本次测试可执行文件到工作树执行，不清理其他代理数据。
- Python 基线：68 tests，OK，27 skipped；有未关闭文件/数据库 ResourceWarning。跳过不计通过。
- Go 基线结果见 iteration-logs/baseline-go-final.log。

- Go 全量基线实际失败：TestX11ClipboardCrossClient 得到空串（已知环境相关问题），其余没有报告失败。后续保留全量结果，不将跳过当通过。

## 第1轮：拒绝数值字面量溢出
证据：ParseInt/ParseFloat 丢弃错误，9223372036854775808 和 400 位小数被接受。新回归先失败（round01-red.log）。
实现：在词法/解析边界返回 E001 与实际行列，保留合法 Int64 最大值。
验证：tools/test-native.sh -run TestIterationNumericOverflow -v，PASS（round01-green.log）。
反思：错误必须发生在最早的输入边界，不能用饱和值掩盖程序错误；暂不扩展任意精度整数。

## 第2轮：解析递归预算
证据：700 层括号、列表及 not 链在原解析器均被无限制接受；回归先失败。
实现：共享 512 层递归预算，覆盖表达式、前缀、模式、块、类型、嵌套视图；超过时 E100，不依靠 recover 拦截不可恢复的栈耗尽。
验证：TestIterationParserDepth 与 TestEveryCodeDocumented 均 PASS；20 层正常语法保留。日志 round02-red/green.log。
反思：继第1轮数值边界后限定结构复杂度；这是深度预算而非执行超时或总 AST 大小预算，后者仍需后续治理。

## 第3轮：WAL 断尾恢复后不再丢新增记录
证据：已有完整记录后接半截 JSON，再打开并写入，下一次打开丢掉新记录；完整 JSON 无换行也有同类问题。损坏结构 {} 会 panic。先红。
实现：仅截去未完成的最后一行；完整末行补分隔符；中部损坏与非法 op/table/id/record 返回错误并保留原文件；传播读取错误，不再当空库。
验证：TestIterationWALRecovery（两种尾部）、TestIterationWALCorruption（三种损坏）及已有迁移测试 PASS，见 round03-green.log。
反思：恢复不能只保证“本次读到”，还须验证恢复后继续写、再重启；未引入跨进程文件锁。

## 第4轮：提交顺序与磁盘故障诚实失败
证据：自动 compact 在 WAL 写完、内存尚未更新时执行，1002 次写入重开仅余 1001 行；关闭底层句柄后 Insert 仍报告成功。两项回归先红。
实现：压缩移到新 WAL 写入之前；写入与 fsync 必须成功才更新内存/ID；失败以新错误码 R060 上抛并封锁该故障句柄后续写入；变更锁 defer 释放，避免异常死锁；压缩检查 flush/sync/close 再 rename。
验证：TestIterationWAL*、TestEveryCodeDocumented、TestMigratePlanApply 全 PASS，见 round04-green.log。
反思：第3轮解决恢复，第4轮补齐运行中提交，避免只测少量数据。未提供事务级多行原子提交；磁盘同步失败后持久化结果本身可能不确定，需备份并重开。

## 第5轮：迁移验证实际回填候选值
证据：新增 Int 字段默认 "bad" 或 nil，计划仍不阻塞；旧计划生成后数据变为违反 invariant，ApplyMigration 仍接受旧计划。先红。
实现：验证 NewRecord 实际候选值（含默认值），非可空 nil 与基础类型不匹配列为 M403；默认表达式求值异常转阻塞；执行前重新计划而非信任外部传入旧步骤。
验证：TestIterationMigrationValidation 和已有 TestMigratePlanApply PASS（round05-green.log）。
反思：检查 schema 不等于检查迁移后的数据。仍为离线迁移；重计划与写入之间没有跨进程锁，不能宣称支持在线并发迁移或多行原子性。

## 第6轮：只读权限不能反而允许写入
证据：auth 只写 can reader read Item 时，reader 的 POST/PATCH/DELETE 因“没有 write 规则”默认放行；先纠正测试中依赖换行的 auth 声明，再用旧实现重跑红测，日志为有效夹具的真实失败。
实现：一旦任何 can 规则命名实体，该实体采用 allowlist；未授权操作默认拒绝。完全没有规则的实体保留原开发默认值，管理员保留权限。
验证：TestIterationReadOnlyPermissions PASS；包括密码注册/会话签名、管理员创建、reader 读取、三种写操作拒绝、无 token 伪造 admin 角色仍 401（round06-green.log）。
反思：这是安全收紧，依赖旧“read 隐含可写”行为的项目应补明确 write/manage；action/flow 仍使用自身授权约定，不宣称全局行级权限。

## 第7轮：MCP 协议边界与参数类型
证据：无效 JSON 被静默丢弃；null/数组/错误版本/非法 ID 与参数没有正确协议错误；大数 ID 经 float64 解码失精度；工具缺必填参数靠内部异常兜底。先红。
实现：区分 -32700/-32600/-32602，保留合法通知无回复；UseNumber 保留 ID；校验工具参数声明、类型、必填字段和未知字段；协议版本只返回实际支持的 2025-06-18，不盲目回显任意版本；输出失败停止服务。旧测试补齐原本缺失的 JSON-RPC 版本。
验证：TestIterationMCPValidation（8 种坏输入、大 ID、通知）与 TestOpenAPIAgentsMCP PASS（round07-green.log）。
反思：协议错误应是机器可判定的数据，工具运行失败仍保留 isError；真实 Cursor 会话未测，执行权限问题留在下一轮单独处理。

## 第8轮：让“只读 MCP”真的不写用户库
证据：aether_migrate_plan 使用普通 OpenStore，加载含顶层 insert 的源码会写入用户库；仅打开大 WAL 或断尾也会压缩/修复源文件；aether_test 可调用 http_get。三种真实副作用先红。
实现：新增无创建/修复/压缩的只读 WAL 快照，写操作明确 R060 拒绝；MCP 迁移使用此快照；执行型 MCP 工具移除 http_get 网络能力；测试工具仍可在内存库正常测试 db 行为，普通 CLI 不受该能力限制影响。
验证：TestIterationMCPReadOnly、TestIterationMCPNoNetwork、协议/旧 MCP 用例与 WAL 回归均 PASS（round08-green.log）。字节级比较原数据库未改变，本地 HTTP 哨兵零请求。
反思：上一轮只保证协议正确，此轮补能力边界；这不是 OS 沙箱，未提供循环/睡眠/内存用量硬上限，不能运行不受信任源码并宣称完全隔离。

## 第9轮：LSP 有界帧与一致性编辑
证据：Content-Length 非数字被当 0、重复头被后者覆盖，写帧忽略 JSON 编码失败；旧版本通知覆盖新文本，半个 UTF-16 代理对及倒置范围被错误应用。先红。
实现：读取限制 8 KiB 头/16 MiB 内容，严格数字与重复头校验；写帧返回编码错误；忽略过期版本，对同通知多次编辑先在临时文本验证，全部成功再提交文本与版本；拒绝越界、倒置和半代理对位置。
验证：TestIterationLSPFraming、TestIterationLSPEditConsistency PASS，涵盖中文及扩展汉字有效替换、多编辑失败不更新版本（round09-green.log）。
反思：LSP 是 IDE 的状态边界，不能只测 hover 成功；16 MiB 超大文件需要拆分，未在 VS Code GUI 实测。

## 第10轮：新手建项目不覆盖源码、不假报成功
证据：aether new existing.ae 无条件覆盖文件；缺失父目录或目标为普通文件时仍报告创建成功；123-中文应用.AE 生成非法模块名。先红。
实现：所有新项目文件使用 O_EXCL 排他创建，检查读取/遍历/建目录/写入/关闭错误；文件均先在内存准备、按稳定顺序写出；单文件模块名规范化并处理大小写扩展名；错误非零退出，不覆盖已有用户数据。
验证：TestIterationNewSafety、TestIterationNewTemplates（全部5模板，包含 AGENTS.md/manifest/调试配置）与已有中文路径回归 PASS（round10-green.log）。
反思：代码生成器也是数据安全边界；失败可能留下已生成的部分文件供检查，未宣称整个项目目录写入具备事务原子性。

## 最终验证与交付
- 首次最终 Go 全量：PASS；TestX11ClipboardCrossClient 本次通过，但 TestX11ClipboardRead 为原有 live-only 跳过，不能计作通过。
- 首次开启本次原生二进制的 Python 全量：68 tests，2 failures、0 skipped；两个旧测试硬编码要求 0.5.x/0.5.1，与基点已有 0.6.0 不符。已修测试为严格比对 native/cli.go 的实际声明版本，而非删掉版本断言；不修改产品版本。详见 final-python.log，修后另存 final-python-rerun.log。
- 测试辅助脚本移除固定 /workspace 路径，非 Linux 无 /dev/shm 时退回普通 go test。
- 最终复查延伸第5/8轮：CLI migrate 也复用了普通 OpenStore，顶层 insert 会在“计划”时写入；默认值函数同样可能写库。补充真实红绿回归 final-migration-safety-red/green.log：CLI 计划使用无修复只读快照、不创建缺失数据库；加载阶段禁止顶层写入，PlanMigration 全程禁止默认值/invariant 写库。--apply 仍需显式授权执行。此项是对已有两轮保证的复查补齐，不另凑轮数。
- 最终复查第1轮的负数对称边界：直接拒绝正向超界幅度会连合法 -9223372036854775808 也拒绝。新增红绿回归 final-minint-red/green.log；仅对该唯一幅度延迟到语法上下文判断，允许负号与负数模式匹配，独立正数及更小负数继续拒绝。未声称处理所有算术运算溢出。

### 最终代码验证矩阵（2026-11-24，Asia/Shanghai）

| 项目 | 真实结果与日志 |
|---|---|
| 原生完整 Go 套件 | `tools/test-native.sh -count=1 -v ./...`：26 个顶层测试通过、1 个原有 live-only 跳过，退出 0；verified-go.log |
| Python + 原生协议/HTTP/DAP/LSP 回归 | 设置 AETHER_NATIVE 指向本次编译二进制，68/68 通过、0 跳过；verified-python-execfs.log。未关闭文件/SQLite/子进程 ResourceWarning 仍存在，未压制。 |
| 语言一致性 | `aether conform conformance/golden.json`：72 passed、0 failed；verified-conformance.log |
| 项目模板 | blank/todo/shop/blog/crm 均 check ok；todo inline 4 passed、0 failed；verified-templates.log |
| Go 静态检查 | `go vet ./...` 退出 0；verified-vet.log |
| 跨平台 | CGO_ENABLED=0，Windows/amd64 与 macOS/amd64 `go build -trimpath` 成功；verified-cross.log；仅交叉编译，未运行 |
| IDE 扩展 | 初次仅装 esbuild 编译因缺 vscode-languageclient 失败；随后按 package-lock `npm ci --ignore-scripts --no-audit --no-fund`，`npm run build` 成功；final-extension-install/rerun.log。node --check 扩展产物与测试 JS 成功；final-extension-static.log。不是 TypeScript 完整类型检查或 GUI 测试。 |
| 安装器 | makensis 使用占位载荷编译 NSIS 脚本成功，final-nsis.log；仅语法/打包脚本验证，绝非可交付的完整 IDE 安装包，也没有运行安装/卸载 |
| Shell 与差异 | sh -n 所列构建/测试脚本及 git diff --check 均成功 |
| 源码包检查 | git 跟踪文件白名单打包；排除 .git、node_modules、缓存、预编译二进制与安装包；常见私钥/token 特征扫描无命中；source-audit.log |

环境问题保留证据：最后完整链曾把 TMPDIR 指到 noexec 的 /dev/shm，导致 Python 部署脚本测试 PermissionError（verified-python.log）。改为工作树内可执行临时目录后，原测试未跳过、68/68 全通过（verified-python-execfs.log）。Go 仍用内存盘缓存/临时文件配合受控复制执行，以避开共享根盘容量压力。

### 兼容性与未验证事项

- 这是本地加固源码，不升版本、不推 GitHub、不建 Release、不更新用户 PC，既有安装包没有包含这些改动。
- 有规则实体现在默认拒绝未授权操作；如确实需写入，补 `can ... write/manage Entity`。`aether new` 不再覆盖已有文件。迁移源码中的顶层写入改用明确的数据初始化命令，不能夹带在计划/迁移加载中。
- 本次主要修改原生 Go 实现。Python 参考运行时仍为仓库原状态（元数据 0.5.0），未同步新增的全部安全边界；72 例一致性通过不等于所有边界行为完全等价。
- MCP 禁止网络且迁移只读，但不是 OS 沙箱，尚无运行时 CPU/循环/睡眠/总内存硬限制。真实第三方智能体 MCP 会话、真实大模型、IDE GUI 启动交互均未验。
- 原生 Windows/macOS 窗口、原生中文输入法仍未实现；没有声称覆盖所有语言/所有平台能力。
- AeStore 仍为单机方案；迁移无跨进程锁、多行事务/在线迁移/断点续跑；未做断电或真实磁盘硬故障测试。故障 fsync 的最终持久化状态可能不确定，应备份重开。
- 基线剪贴板跨客户端测试失败，最终完整运行通过，仍按环境相关偶发问题记录；live-only 读取测试跳过，不计入通过。
- NSIS 为占位载荷静态验证，完整 Electron 外壳未重建、Windows 真机安装/卸载与代码签名未验；源码不含 VSCodium 全量源码。扩展现有 npm test 指向缺失的 test/runTest.js，未伪称 GUI 自动测试通过。
- 源码 ZIP、完整中文说明、补丁、验证日志与 SHA256 放在 /workspace/delivery/aether-iteration10。只包含源码与文本证据，字体/图标资源及许可证保留。
