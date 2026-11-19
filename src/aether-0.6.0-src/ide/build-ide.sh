#!/usr/bin/env bash
# Build "Aether IDE" 0.6.0 from prebuilt VSCodium binaries (Code - OSS based, MIT) — rebrand + built-in Aether extension.
#   VSCODIUM_DIR=/path/with/VSCodium-*.tar.gz|zip  ./build-ide.sh [outdir]
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd); ROOT=$(cd "$HERE/.." && pwd)
OUT=${1:-$ROOT/release}; V=0.6.0; VC=${VSCODIUM_VERSION:-1.135.06055}; DL=${VSCODIUM_DIR:-/workspace/ide-dl}
W=$(mktemp -d); mkdir -p "$OUT"
EXT=$ROOT/editors/vscode; NB=$ROOT/native/build
(cd "$EXT" && npx esbuild src/extension.ts --bundle --outfile=dist/extension.js --external:vscode --format=cjs --platform=node --target=node18 --minify >/dev/null)

icons() { # $1 = dir to write aether-ide.png/.ico/.icns
  python3 - "$1" "$HERE" <<'PY'
import sys
from PIL import Image
d, h = sys.argv[1], sys.argv[2]
big = Image.open(f"{h}/aether-icon-1024.png")
Image.open(f"{h}/aether-icon-512.png").save(f"{d}/aether-ide.png")
big.save(f"{d}/aether-ide.ico", sizes=[(16,16),(24,24),(32,32),(48,48),(64,64),(128,128),(256,256)])
big.save(f"{d}/aether-ide.icns")
PY
}

# ---------------- Linux x64 ----------------
L=$W/aether-ide-linux-x64; mkdir -p "$L"; tar xzf "$DL/VSCodium-linux-x64-$VC.tar.gz" -C "$L"
python3 "$HERE/brand.py" "$L/resources/app" linux-x64 "$NB/aether-linux-amd64" "$EXT"
icons "$W"; cp "$W/aether-ide.png" "$L/resources/app/resources/linux/code.png"
mv "$L/codium" "$L/aether-ide"; rm -f "$L/bin/codium" "$L/bin/codium-tunnel"
cat > "$L/bin/aether-ide" <<'SH'
#!/usr/bin/env sh
# Aether IDE launcher (based on the VSCodium/Code-OSS launcher)
VSCODE_PATH="$(dirname "$(dirname "$(readlink -f "$0")")")"
ELECTRON="$VSCODE_PATH/aether-ide"
CLI="$VSCODE_PATH/resources/app/out/cli.js"
ELECTRON_RUN_AS_NODE=1 "$ELECTRON" "$CLI" "$@"
exit $?
SH
chmod +x "$L/bin/aether-ide"
cp "$NB/aether-linux-amd64" "$L/bin/aether"   # the native language runtime on PATH next to the IDE
cp "$HERE/README-IDE.md" "$L/README-Aether-IDE.md"
tar czf "$OUT/Aether-IDE-$V-linux-x64.tar.gz" -C "$W" aether-ide-linux-x64
# .deb
D=$W/deb; mkdir -p "$D/DEBIAN" "$D/usr/share" "$D/usr/bin" "$D/usr/share/applications" "$D/usr/share/pixmaps"
cp -a "$L" "$D/usr/share/aether-ide"
ln -s /usr/share/aether-ide/bin/aether-ide "$D/usr/bin/aether-ide"; ln -s /usr/share/aether-ide/bin/aether "$D/usr/bin/aether"
cp "$W/aether-ide.png" "$D/usr/share/pixmaps/aether-ide.png"
cat > "$D/usr/share/applications/aether-ide.desktop" <<DESK
[Desktop Entry]
Name=Aether IDE
Comment=Aether 语言 IDE（人与智能体协作）
Exec=/usr/share/aether-ide/aether-ide %F
Icon=aether-ide
Type=Application
StartupNotify=false
StartupWMClass=Aether IDE
Categories=TextEditor;Development;IDE;
MimeType=text/plain;
Keywords=aether;ide;
DESK
SIZE=$(du -sk "$D/usr" | cut -f1)
cat > "$D/DEBIAN/control" <<CTL
Package: aether-ide
Version: $V
Section: devel
Priority: optional
Architecture: amd64
Installed-Size: $SIZE
Maintainer: Aether contributors <noreply@example.invalid>
Depends: libnss3, libgtk-3-0 | libgtk-4-1, libxkbfile1, libsecret-1-0, libgbm1, libasound2 | libasound2t64
Description: Aether IDE — Code-OSS (VSCodium) based IDE with built-in Aether language support
 Bundles the native aether runtime and LSP server, Aether extension (preview, agent panel, history, tests).
CTL
dpkg-deb --root-owner-group --build "$D" "$OUT/aether-ide_${V}_amd64.deb" >/dev/null
# AppImage (optional: needs appimagetool)
if [ -n "${APPIMAGETOOL:-}" ]; then
  A=$W/AppDir; mkdir -p "$A/usr/share"; cp -a "$L" "$A/usr/share/aether-ide"
  cp "$W/aether-ide.png" "$A/aether-ide.png"; cp "$D/usr/share/applications/aether-ide.desktop" "$A/aether-ide.desktop"
  sed -i 's|^Exec=.*|Exec=aether-ide %F|' "$A/aether-ide.desktop"
  printf '#!/bin/sh\nHERE="$(dirname "$(readlink -f "$0")")"\nexec "$HERE/usr/share/aether-ide/aether-ide" "$@"\n' > "$A/AppRun"; chmod +x "$A/AppRun"
  ARCH=x86_64 "$APPIMAGETOOL" --appimage-extract-and-run "$A" "$OUT/Aether-IDE-$V-x86_64.AppImage" >/dev/null 2>&1 || ARCH=x86_64 "$APPIMAGETOOL" "$A" "$OUT/Aether-IDE-$V-x86_64.AppImage"
fi

# ---------------- Windows x64 (portable zip + per-user installer script) ----------------
if [ -f "$DL/VSCodium-win32-x64-$VC.zip" ]; then
  Wn=$W/aether-ide-win32-x64; mkdir -p "$Wn"; (cd "$Wn" && unzip -q "$DL/VSCodium-win32-x64-$VC.zip")
  python3 "$HERE/brand.py" "$Wn/resources/app" win32-x64 "$NB/aether-windows-amd64.exe" "$EXT"
  cp "$W/aether-ide.ico" "$Wn/resources/app/resources/win32/code.ico" 2>/dev/null || true
  mv "$Wn/VSCodium.exe" "$Wn/Aether IDE.exe"
  python3 "$HERE/peicon.py" "$Wn/Aether IDE.exe" "$HERE/aether-icon-1024.png" || echo "warning: exe icon not replaced (pip install pefile)"
  rm -f "$Wn/bin/codium" "$Wn/bin/codium.cmd"
  printf '@echo off\r\nsetlocal\r\nset VSCODE_DEV=\r\nset ELECTRON_RUN_AS_NODE=1\r\n"%%~dp0..\\Aether IDE.exe" "%%~dp0..\\resources\\app\\out\\cli.js" %%*\r\nendlocal\r\n' > "$Wn/bin/aether-ide.cmd"
  cp "$NB/aether-windows-amd64.exe" "$Wn/bin/aether.exe"
  cp "$HERE/install-ide.ps1" "$Wn/install.ps1"; printf '@echo off\r\npowershell -ExecutionPolicy Bypass -File "%%~dp0install.ps1"\r\npause\r\n' > "$Wn/install.bat"
  cp "$HERE/README-IDE.md" "$Wn/README-Aether-IDE.md"
  (cd "$W" && rm -f "$OUT/Aether-IDE-$V-win32-x64.zip" && zip -qr "$OUT/Aether-IDE-$V-win32-x64.zip" aether-ide-win32-x64)
fi

# ---------------- macOS arm64 (unsigned .app in zip) ----------------
if [ -f "$DL/VSCodium-darwin-arm64-$VC.zip" ]; then
  M=$W/mac; mkdir -p "$M"; (cd "$M" && unzip -q "$DL/VSCodium-darwin-arm64-$VC.zip")
  mv "$M/VSCodium.app" "$M/Aether IDE.app"; R="$M/Aether IDE.app/Contents/Resources"
  python3 "$HERE/brand.py" "$R/app" darwin-arm64 "$NB/aether-darwin-arm64" "$EXT"
  cp "$W/aether-ide.icns" "$R/VSCodium.icns"
  python3 - "$M/Aether IDE.app/Contents/Info.plist" <<'PY'
import plistlib, sys
p = sys.argv[1]; d = plistlib.load(open(p, "rb"))
d.update({"CFBundleName": "Aether IDE", "CFBundleDisplayName": "Aether IDE", "CFBundleIdentifier": "dev.aether-lang.ide", "CFBundleShortVersionString": d.get("CFBundleShortVersionString", "") + "+aether0.6.0"})
plistlib.dump(d, open(p, "wb"))
PY
  cp "$NB/aether-darwin-arm64" "$R/app/bin/aether"
  cp "$HERE/README-IDE.md" "$M/README-Aether-IDE.md"
  (cd "$M" && rm -f "$OUT/Aether-IDE-$V-darwin-arm64.zip" && zip -qry "$OUT/Aether-IDE-$V-darwin-arm64.zip" "Aether IDE.app" README-Aether-IDE.md)
fi
echo "$W"
