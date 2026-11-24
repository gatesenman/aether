#!/bin/sh
# Linux low-disk helper. Ordinary builds may simply use `cd native && go test ./...`.
set -eu
cd "$(dirname "$0")/../native"
export CGO_ENABLED=0
if [ -d /dev/shm ] && [ -w /dev/shm ]; then
 export GOCACHE=${GOCACHE:-/dev/shm/aether-iteration10-cache}
 export GOTMPDIR=${GOTMPDIR:-/dev/shm/aether-iteration10-tmp}
 export TMPDIR=${TMPDIR:-$GOTMPDIR}
 mkdir -p "$GOCACHE" "$GOTMPDIR"
 exec go test -exec ../tools/run-test-binary.sh "$@"
fi
exec go test "$@"
