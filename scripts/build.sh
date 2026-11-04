#!/usr/bin/env bash
# 交叉编译全部平台，输出到 dist/。版本号取 VERSION 环境变量，没有就用 git describe。
set -euo pipefail
cd "$(dirname "$0")/.."
ver="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
rm -rf dist && mkdir -p dist
for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  os=${t%/*}; arch=${t#*/}; ext=""; [ "$os" = windows ] && ext=".exe"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildVersion=$ver" \
    -o "dist/cchud-$os-$arch$ext" .
  echo "dist/cchud-$os-$arch$ext"
done
(cd dist && if command -v sha256sum >/dev/null; then sha256sum cchud-*; else shasum -a 256 cchud-*; fi > SHA256SUMS)
echo "dist/SHA256SUMS"
