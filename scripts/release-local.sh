#!/usr/bin/env bash
# 在本机发布 Release，用于 GitHub Actions 不可用的时候。结果和 .github/workflows/release.yml 相同。
#
# 用法：scripts/release-local.sh v0.1.0
#
# 步骤：检查工作区干净且已推送，跑全部测试，编译 6 个平台，
# 通过 GitHub API 在当前 main 提交上创建标签和 Release，上传文件和 SHA256SUMS。
# 令牌取 GH_TOKEN 或 GITHUB_TOKEN 环境变量，没有就用 git 凭据里 github.com 的令牌，需要 repo 权限。
set -euo pipefail
cd "$(dirname "$0")/.."

tag="${1:-}"
case "$tag" in
  v[0-9]*) ;;
  *) echo "用法: $0 vX.Y.Z" >&2; exit 2 ;;
esac

remote_url="$(git remote get-url origin)"
repo="$(printf '%s' "$remote_url" | sed -E 's#^(https://github.com/|git@github.com:)##; s#\.git$##')"

if [ -n "$(git status --porcelain)" ]; then
  echo "工作区有未提交的改动，先提交。" >&2; exit 1
fi
git fetch -q origin
sha="$(git rev-parse HEAD)"
if [ "$sha" != "$(git rev-parse origin/main)" ]; then
  echo "当前提交和 origin/main 不一致，先推送 main。" >&2; exit 1
fi

token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [ -z "$token" ]; then
  token="$(printf 'protocol=https\nhost=github.com\n\n' | git credential fill 2>/dev/null | sed -n 's/^password=//p')"
fi
[ -n "$token" ] || { echo "没有可用的 GitHub 令牌。" >&2; exit 1; }
api() { curl -sS -H "Authorization: Bearer $token" -H "Accept: application/vnd.github+json" "$@"; }

if [ "$(api -o /dev/null -w '%{http_code}' "https://api.github.com/repos/$repo/releases/tags/$tag")" = 200 ]; then
  echo "Release $tag 已存在。" >&2; exit 1
fi

echo "== 测试"
go vet ./...
CCHUD_E2E=1 go test -count=1 ./...

echo "== 编译"
VERSION="$tag" ./scripts/build.sh
./dist/cchud-"$(go env GOOS)"-"$(go env GOARCH)" version

echo "== 创建 Release ${tag}（${repo}，提交 ${sha:0:7}）"
notes="安装或升级（会自动修改 Claude Code 的 settings.json，修改前先备份）：

macOS / Linux:
\`\`\`sh
curl -fsSL https://raw.githubusercontent.com/$repo/main/scripts/install.sh | bash
\`\`\`

Windows (PowerShell):
\`\`\`powershell
irm https://raw.githubusercontent.com/$repo/main/scripts/install.ps1 | iex
\`\`\`

文件校验值见 SHA256SUMS。"
body="$(TAG="$tag" SHA="$sha" NOTES="$notes" python3 -c 'import json,os;print(json.dumps({"tag_name":os.environ["TAG"],"target_commitish":os.environ["SHA"],"name":os.environ["TAG"],"body":os.environ["NOTES"]}))')"
resp="$(api -X POST "https://api.github.com/repos/$repo/releases" -d "$body")"
id="$(printf '%s' "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id") or "")')"
if [ -z "$id" ]; then
  echo "创建 Release 失败: $resp" >&2; exit 1
fi

for f in dist/*; do
  name="$(basename "$f")"
  code="$(api -o /dev/null -w '%{http_code}' -X POST -H "Content-Type: application/octet-stream" \
    --data-binary @"$f" "https://uploads.github.com/repos/$repo/releases/$id/assets?name=$name")"
  [ "$code" = 201 ] || { echo "上传 $name 失败，HTTP $code" >&2; exit 1; }
  echo "已上传 $name"
done
git fetch -q --tags origin
echo "完成: https://github.com/$repo/releases/tag/$tag"
