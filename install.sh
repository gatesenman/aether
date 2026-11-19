#!/usr/bin/env sh
# Aether unified installer: runtime + SDK + CLI + Studio IDE. Requires Python 3.9+.
set -e
cd "$(dirname "$0")"
PY=${PYTHON:-python3}
PREFIX=${AETHER_HOME:-$HOME/.aether}
BIN=${AETHER_BIN:-$HOME/.local/bin}
$PY -c 'import sys; assert sys.version_info >= (3,9), "Python 3.9+ required"'
$PY -m venv "$PREFIX"
"$PREFIX/bin/pip" install --quiet --no-index dist/*.whl
mkdir -p "$BIN"; ln -sf "$PREFIX/bin/aether" "$BIN/aether"
echo "Aether installed -> $BIN/aether"
case ":$PATH:" in *":$BIN:"*) ;; *) echo "提示: 请把 $BIN 加入 PATH  (export PATH=\"$BIN:\$PATH\")";; esac
"$PREFIX/bin/aether" version
