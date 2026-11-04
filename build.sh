#!/usr/bin/env bash
# 交叉编译全部平台，输出到 dist/。
set -euo pipefail
cd "$(dirname "$0")"
ver="$(git describe --always --dirty 2>/dev/null || echo dev)-$(date +%Y%m%d%H%M)"
rm -rf dist && mkdir -p dist
for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  os=${t%/*}; arch=${t#*/}; ext=""; [ "$os" = windows ] && ext=".exe"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildVersion=$ver" \
    -o "dist/cchud-$os-$arch$ext" .
  echo "dist/cchud-$os-$arch$ext"
done
