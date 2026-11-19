#!/usr/bin/env sh
# Aether 0.6.0 原生安装器：无需 Python、无需浏览器。用法：sh install.sh [安装目录，默认 ~/.local]
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"; PREFIX="${1:-$HOME/.local}"
mkdir -p "$PREFIX/bin" "$PREFIX/share/aether"
cp "$HERE/aether" "$PREFIX/bin/aether"; chmod +x "$PREFIX/bin/aether"
cp -r "$HERE/examples" "$HERE/docs" "$HERE/conformance" "$PREFIX/share/aether/"
"$PREFIX/bin/aether" version
"$PREFIX/bin/aether" conform "$PREFIX/share/aether/conformance/golden.json" | tail -1
for ed in code codium cursor aether-ide; do
  if command -v $ed >/dev/null 2>&1 && ls "$HERE"/aether-vscode-*.vsix >/dev/null 2>&1; then
    $ed --install-extension "$(ls "$HERE"/aether-vscode-*.vsix | head -1)" --force >/dev/null 2>&1 && echo "已为 $ed 安装 Aether 扩展"
  fi
done
case ":$PATH:" in *":$PREFIX/bin:"*) ;; *) echo "提示：把 $PREFIX/bin 加入 PATH：export PATH=\"$PREFIX/bin:\$PATH\"";; esac
echo "安装完成。试试：aether ui $PREFIX/share/aether/examples/todo.ae   或   aether studio 我的应用.ae"
