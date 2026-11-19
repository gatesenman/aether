#!/usr/bin/env sh
# Build the Aether VS Code extension: one universal .vsix (uses PATH/aether.server.path) + platform .vsix with the native binary.
set -e; cd "$(dirname "$0")"; OUT=${1:-../dist}; mkdir -p $OUT; NB=../../native/build
npx esbuild src/extension.ts --bundle --outfile=dist/extension.js --external:vscode --format=cjs --platform=node --target=node18 --minify
rm -rf bin; npx vsce package --no-dependencies -o $OUT/aether-vscode-0.6.0.vsix
for t in linux-x64:linux:amd64:linux-x64 linux-arm64:linux:arm64:linux-arm64 win32-x64:windows:amd64:win32-x64 win32-arm64:windows:arm64:win32-arm64 darwin-x64:darwin:amd64:darwin-x64 darwin-arm64:darwin:arm64:darwin-arm64; do
  T=${t%%:*}; r=${t#*:}; GOOS=${r%%:*}; r=${r#*:}; GOARCH=${r%%:*}; NODEP=${r#*:}
  ext=; [ $GOOS = windows ] && ext=.exe
  rm -rf bin; mkdir -p bin/$NODEP; cp $NB/aether-$GOOS-$GOARCH$ext bin/$NODEP/aether$ext; chmod +x bin/$NODEP/aether$ext
  npx vsce package --no-dependencies --target $T -o $OUT/aether-vscode-0.6.0-$T.vsix
done
rm -rf bin
