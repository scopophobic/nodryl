#!/usr/bin/env sh
set -eu

platform="$(go env GOOS)-$(go env GOARCH)"
target="npm/$platform/bin/nodryl"
mkdir -p "npm/$platform/bin"
go build -o "$target" ./cmd/nodryl
chmod 755 "$target"
printf 'Built %s\n' "$target"
