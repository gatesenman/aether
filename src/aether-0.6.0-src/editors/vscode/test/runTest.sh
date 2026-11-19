#!/usr/bin/env sh
# Headless integration test of the extension inside VSCodium/VS Code under Xvfb.
#   CODE=/path/to/VSCodium/codium sh test/runTest.sh   (the electron binary, NOT bin/codium which detaches)
set -e; cd "$(dirname "$0")/.."; EXT=$(pwd)
WS=$(mktemp -d); cp ../../examples/*.ae $WS/
NB=../../native/build/aether-linux-amd64; mkdir -p bin/linux-x64; cp $NB bin/linux-x64/aether
CODE=${CODE:-/workspace/ide-build/codium-linux/codium}
case "$CODE" in */bin/codium) CODE="$(dirname "$(dirname "$CODE")")/codium";; esac
OUT=${AETHER_TEST_OUT:-$WS/results.json}
AETHER_TEST_WS=$WS AETHER_TEST_OUT=$OUT "$CODE" --no-sandbox --disable-gpu --disable-workspace-trust --skip-welcome --skip-release-notes \
  --user-data-dir $WS/.ud --extensions-dir $WS/.ext --extensionDevelopmentPath=$EXT --extensionTestsPath=$EXT/test/suite/index.js $WS || true
cat $OUT
python3 -c "import json,sys;r=json.load(open('$OUT'));f=[x for x in r if not x['ok']];print(f'{len(r)-len(f)}/{len(r)} passed');sys.exit(1 if f else 0)"
