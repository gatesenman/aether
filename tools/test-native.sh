#!/bin/sh
set -eu
cd "$(dirname "$0")/../native"
export CGO_ENABLED=0
export GOCACHE=${GOCACHE:-/dev/shm/aether-iteration10-cache}
export GOTMPDIR=${GOTMPDIR:-/dev/shm/aether-iteration10-tmp}
export TMPDIR=${TMPDIR:-/dev/shm/aether-iteration10-tmp}
exec go test -exec ../tools/run-test-binary.sh "$@"
