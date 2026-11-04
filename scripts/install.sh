#!/usr/bin/env bash
# 从 GitHub Release 下载 cchud，校验 SHA256 后安装，再运行 `cchud setup`（macOS、Linux）。
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/BigerPatax/cchud/main/scripts/install.sh | bash
#
# 可选环境变量：
#   CCHUD_VERSION=v0.1.0     安装指定版本，默认最新版
#   CCHUD_INSTALL_DIR=路径   安装目录，默认 ~/.local/bin
#   CCHUD_SKIP_SETUP=1       只安装程序，不改 Claude Code 的 settings.json
set -euo pipefail

repo="BigerPatax/cchud"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  darwin|linux) ;;
  *) echo "cchud: 不支持的系统 ${os}。Windows 请用 scripts/install.ps1。" >&2; exit 1 ;;
esac
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "cchud: 不支持的 CPU 架构 ${arch}。" >&2; exit 1 ;;
esac

ver="${CCHUD_VERSION:-latest}"
if [ "$ver" = latest ]; then
  base="https://github.com/$repo/releases/latest/download"
else
  base="https://github.com/$repo/releases/download/$ver"
fi
asset="cchud-$os-$arch"
dest="${CCHUD_INSTALL_DIR:-$HOME/.local/bin}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "下载 $base/$asset"
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"

expected="$(awk -v f="$asset" '$2 == f || $2 == "*"f {print $1}' "$tmp/SHA256SUMS")"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "cchud: SHA256 校验失败，未安装。期望 ${expected:-空}，实际 ${actual}。" >&2
  exit 1
fi
echo "SHA256 校验通过"

mkdir -p "$dest"
# 先复制到同一目录下的临时文件再改名，正在运行的旧版本不受影响。
cp "$tmp/$asset" "$dest/cchud.tmp"
chmod 755 "$dest/cchud.tmp"
mv -f "$dest/cchud.tmp" "$dest/cchud"
echo "已安装 $dest/cchud"
"$dest/cchud" version

if [ "${CCHUD_SKIP_SETUP:-}" = 1 ]; then
  echo "按 CCHUD_SKIP_SETUP=1 跳过 setup。手动配置见 README。"
else
  "$dest/cchud" setup
fi
