#!/usr/bin/env sh
# Build wheel + unified archive into ./release
set -e; cd "$(dirname "$0")"
rm -rf build dist release aether/examples *.egg-info; mkdir -p aether/examples release
cp examples/*.ae aether/examples/
python3 mkwheel.py
V=aether-0.5.0; S=release/$V; mkdir -p $S
cp -r dist docs examples tests screenshots README.md install.sh install.bat pyproject.toml build.sh mkwheel.py $S/
cp -r aether $S/aether; find $S -name __pycache__ -prune -exec rm -rf {} +
tar czf release/$V.tar.gz -C release $V
(cd release && python3 -m zipfile -c $V.zip $V)
cp dist/*.whl release/; rm -rf $S
ls -la release
