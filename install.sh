#!/usr/bin/env bash
# 在 macOS 或 Linux 上编译并安装到 ~/.local/bin/cchud。
# 先写临时文件再改名替换：直接覆盖正在运行的程序文件，在 macOS 上会让旧进程被系统杀掉。
set -euo pipefail
cd "$(dirname "$0")"
dest="${CCHUD_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$dest"
ver="$(git describe --always --dirty 2>/dev/null || echo dev)-$(date +%Y%m%d%H%M)"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildVersion=$ver" -o "$dest/cchud.tmp" .
mv -f "$dest/cchud.tmp" "$dest/cchud"
echo "已安装 $dest/cchud ($ver)。旧的常驻进程会在下一次状态栏刷新时自动换成新版本。"
