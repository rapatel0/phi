#!/usr/bin/env bash
# Rebuild the WASM guests under internal/ext/wasmhost/guests into testdata/.
#
# Guests are committed as built modules so `go test ./...` works without a
# second toolchain. Re-run this after changing a guest source, then commit both.
#
# Usage: scripts/build-guests.sh
set -euo pipefail

cd "$(dirname "$0")/.."
OUT=internal/ext/wasmhost/testdata

for src in internal/ext/wasmhost/guests/*/; do
	name=$(basename "$src")
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -ldflags="-s -w" \
		-o "$OUT/$name.wasm" "./$src"
	echo "built $OUT/$name.wasm"
done
