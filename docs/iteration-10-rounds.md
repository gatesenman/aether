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
实现：词法器返回 E001 与实际行列，保留合法 Int64 最大值。
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

