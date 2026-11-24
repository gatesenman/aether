#!/bin/sh
# 内存盘禁止执行时，只把本次测试二进制复制到工作树执行。
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
dir=${AETHER_TEST_BIN_DIR:-$root/iteration-logs}
mkdir -p "$dir"
bin=$(mktemp "$dir/test-exec.XXXXXX")
trap 'rm -f "$bin"' EXIT HUP INT TERM
cp "$1" "$bin"
chmod +x "$bin"
shift
"$bin" "$@"
