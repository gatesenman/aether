#!/usr/bin/env sh
# End-to-end beginner scenario in the built Aether IDE under Xvfb, with real screen captures.
#   IDE=/path/to/aether-ide-linux-x64 DISPLAY=:77 sh test/e2e.sh
set -e; cd "$(dirname "$0")/.."; EXT=$(pwd)
IDE=${IDE:?set IDE to the unpacked Aether IDE dir}; BIN="$IDE/aether-ide"; [ -x "$BIN" ] || BIN=$(ls "$IDE"/aether-ide* "$IDE"/codium 2>/dev/null | head -1)
W=$(mktemp -d); SH=${AETHER_E2E_SHOTS:-$EXT/../../screenshots}; mkdir -p "$SH"
PROJ=$W/AetherProjects/客户管理; mkdir -p "$W/AetherProjects" "$W/.ud/User"
NBIN=$(cd "$EXT/../../native/build" && pwd)/aether-linux-amd64
printf '{"aether.server.path": "%s", "aether.projectsDir": "%s", "workbench.startupEditor": "none", "window.newWindowDimensions": "fullscreen", "security.workspace.trust.enabled": false}\n' "$NBIN" "$W/AetherProjects" > "$W/.ud/User/settings.json"
run() { PATH=$EXT/../../native/build/pathbin:$PATH AETHER_E2E_SHOTS=$SH AETHER_E2E_OUT=$W/$1.json AETHER_E2E_PROJECT=$PROJ HOME=$W timeout 300 "$BIN" --no-sandbox --disable-gpu --disable-workspace-trust \
  --skip-release-notes --user-data-dir $W/.ud --extensions-dir $W/.ext --extensionDevelopmentPath=$EXT --extensionTestsPath=$EXT/test/e2e/$1.js $2 >$W/$1.log 2>&1 || true; }
run phase1
run phase2 "$PROJ"
python3 - "$W" <<'P'
import json,sys,os
ok=True
for ph in ("phase1","phase2"):
    p=os.path.join(sys.argv[1],ph+".json")
    r=json.load(open(p)) if os.path.exists(p) else []
    for x in r: print(("PASS " if x["ok"] else "FAIL ")+ph+": "+x["name"]+("" if x["ok"] else "\n   "+x["error"][:400]))
    ok = ok and bool(r) and all(x["ok"] for x in r)
print("E2E", "OK" if ok else "FAILED"); sys.exit(0 if ok else 1)
P
