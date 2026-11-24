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

