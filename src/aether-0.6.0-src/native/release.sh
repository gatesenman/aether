#!/usr/bin/env sh
# 构建 Aether 0.6.0 全平台原生发布包 → ../release
set -e; cd "$(dirname "$0")"; R=${R:-../release-0.6.0}; V=0.6.0; mkdir -p $R
for t in linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64; do
  o=${t%/*}; a=${t#*/}; N=aether-$V-$o-$a; S=$R/$N; rm -rf $S; mkdir -p $S
  ext=; [ $o = windows ] && ext=.exe
  CGO_ENABLED=0 GOOS=$o GOARCH=$a go build -trimpath -ldflags "-s -w" -o $S/aether$ext .
  cp -r ../examples ../docs $S/; mkdir -p $S/conformance; cp -r ../conformance/golden.json ../conformance/programs $S/conformance/
  cp assets/FONTS-LICENSE.txt assets/CODICONS-LICENSE.txt ../README.md $S/
  vt=$(echo $o-$a | sed 's/windows/win32/;s/amd64/x64/'); [ -f ../editors/dist/aether-vscode-$V-$vt.vsix ] && cp ../editors/dist/aether-vscode-$V-$vt.vsix $S/ || true
  if [ $o = windows ]; then cp pkg/install.ps1 pkg/install.bat $S/; (cd $R && rm -f $N.zip && python3 -m zipfile -c $N.zip $N)
  else cp pkg/install.sh $S/; tar czf $R/$N.tar.gz -C $R $N; fi
  rm -rf $S; echo "built $N"
done
# 源码包（Go 原生实现 + Python 规范实现 + 一致性套件）
SRC=$R/aether-$V-src; rm -rf $SRC; mkdir -p $SRC
cp -r ../aether ../tests ../examples ../docs ../conformance ../README.md ../pyproject.toml ../mkwheel.py ../build.sh ../install.sh ../install.bat $SRC/
mkdir -p $SRC/native; cp -r *.go go.mod go.sum assets pkg templates release.sh $SRC/native/; cp -r ../installer $SRC/installer
mkdir -p $SRC/editors/vscode $SRC/ide
(cd ../editors/vscode && tar cf - --exclude=node_modules --exclude=bin --exclude='*.vsix' .) | (cd $SRC/editors/vscode && tar xf -)
cp ../ide/*.py ../ide/*.sh ../ide/*.ps1 ../ide/*.md ../ide/aether-icon-*.png $SRC/ide/
find $SRC -name __pycache__ -prune -exec rm -rf {} +
tar czf $R/aether-$V-src.tar.gz -C $R aether-$V-src; rm -rf $SRC
(cd $R && sha256sum aether-$V-* > SHA256SUMS-$V.txt)
