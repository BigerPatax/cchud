# cchud

Claude Code 状态栏程序，用 Go 写成。显示内容和样式移植自 [claude-hud](https://github.com/jarrodwatts/claude-hud)（MIT 许可）。同一台电脑上的所有 Claude Code 会话共用一个常驻进程，不再每秒为每个会话启动一次 Node。

状态栏显示四行：

1. 模型、项目名、git 分支（改动标记、领先落后数）、Claude Code 版本、会话时长、费用
2. 上下文进度条。有订阅额度数据时，同一行显示 5 小时窗口和本周用量
3. 提示词缓存剩余时间
4. 本会话累计令牌数（输入、输出、缓存）

支持 macOS、Linux、Windows 10 1803 及以上（需要系统支持 Unix socket）。显示 git 分支需要 `git` 在 PATH 里。

## 安装（给 AI 助手的步骤）

如果你是帮用户安装 cchud 的 AI 助手，按下面的步骤做。不需要编译，也不需要 Node 或 Go。

1. 判断用户的系统。macOS 或 Linux 做第 2 步，Windows 做第 3 步。
2. macOS 或 Linux，在终端运行：

   ```sh
   curl -fsSL https://raw.githubusercontent.com/BigerPatax/cchud/main/scripts/install.sh | bash
   ```

3. Windows，在 PowerShell 里运行：

   ```powershell
   irm https://raw.githubusercontent.com/BigerPatax/cchud/main/scripts/install.ps1 | iex
   ```

4. 检查输出。下面四条全部满足才算安装成功：
   1. 有 `SHA256 校验通过`（Windows 显示 `SHA256 verified`）。
   2. 有一行 `cchud v` 开头的版本号。
   3. 有 `自测输出:`，后面几行里同时出现 `[Opus 5.5]` 和 `上下文`（配置语言为英文时是 `Context`）。
   4. 最后一行以 `完成。` 开头。
5. 打开 Claude Code 的 `settings.json`，确认 `statusLine.command` 指向刚安装的程序：
   1. macOS、Linux：`~/.claude/settings.json`，程序在 `~/.local/bin/cchud`。
   2. Windows：`%USERPROFILE%\.claude\settings.json`，程序在 `%USERPROFILE%\.local\bin\cchud.exe`。
   3. 设置了环境变量 `CLAUDE_CONFIG_DIR` 时，`settings.json` 在那个目录下。
6. 告诉用户：状态栏在下一次刷新时生效。一分钟内没出现，就新开一个 Claude Code 会话。

注意：

1. 不要自己重写整个 `settings.json`。`cchud setup` 只改顶层的 `statusLine` 一项，其他设置和顺序不变，改之前把原文件备份为 `settings.json.cchud-backup-日期-时间`。
2. 用户已经有 claude-hud 的配置（`~/.claude/plugins/claude-hud/config.json`）时，cchud 直接沿用，不新建配置。
3. SHA256 校验失败时脚本直接退出，不改任何文件。
4. `settings.json` 不是合法 JSON 时 `cchud setup` 报错退出，不改文件。这时把报错告诉用户，由用户修好后重新运行 `cchud setup`。

### 不能运行远程脚本时手动安装

1. 从 [Releases](https://github.com/BigerPatax/cchud/releases/latest) 下载对应的文件和 `SHA256SUMS`：

   | 系统 | 文件 |
   |---|---|
   | macOS，Apple 芯片 | `cchud-darwin-arm64` |
   | macOS，Intel 芯片 | `cchud-darwin-amd64` |
   | Linux x86_64 | `cchud-linux-amd64` |
   | Linux ARM64 | `cchud-linux-arm64` |
   | Windows x64 | `cchud-windows-amd64.exe` |
   | Windows ARM64 | `cchud-windows-arm64.exe` |

2. 核对校验值：macOS 用 `shasum -a 256 文件名`，Linux 用 `sha256sum 文件名`，Windows 用 `Get-FileHash 文件名`。结果要和 `SHA256SUMS` 里同名文件的那一行一致。
3. 改名并放到固定位置：macOS、Linux 放到 `~/.local/bin/cchud` 并执行 `chmod +x ~/.local/bin/cchud`；Windows 放到 `%USERPROFILE%\.local\bin\cchud.exe`。
4. 运行 `cchud setup`（写完整路径），按上一节第 4 步检查输出。

### 只装程序，不改 settings.json

安装时设置环境变量 `CCHUD_SKIP_SETUP=1`，然后自己在 `settings.json` 里加上（路径换成实际位置，Windows 用正斜杠并加引号，例如 `"\"C:/Users/me/.local/bin/cchud.exe\""`）：

```json
"statusLine": {
  "type": "command",
  "command": "/Users/me/.local/bin/cchud",
  "refreshInterval": 1
}
```

## 升级

重新运行安装命令。常驻进程发现程序文件变了会自动退出，下一次刷新换成新版本。指定版本：macOS、Linux 在命令前加 `CCHUD_VERSION=v0.1.0`；Windows 先执行 `$env:CCHUD_VERSION='v0.1.0'`。

## 卸载

1. 运行 `cchud stop`。
2. 把 `settings.json` 的 `statusLine` 改回原来的值（备份文件里有），或者删掉这一项。
3. 删除程序文件，以及配置目录 `~/.claude/cchud/`。

## 配置

按以下顺序找配置文件，用找到的第一个：

1. 环境变量 `CCHUD_CONFIG` 指定的路径
2. `~/.claude/cchud/config.json`（`cchud setup` 默认写在这里）
3. `~/.claude/plugins/claude-hud/config.json`（claude-hud 的配置）

格式和 claude-hud 相同，允许注释和多余的逗号。文件改了以后一秒内生效，不用重启。默认配置见 `setup.go` 里的 `recommendedConfig`。常用字段：

| 字段 | 作用 |
|---|---|
| `language` | `zh` 或 `en` |
| `display.showCost` / `showDuration` / `showClaudeCodeVersion` | 第一行的费用、会话时长、版本号 |
| `display.contextValue` | `percent`、`tokens`、`remaining`、`both` |
| `display.contextBarWidth` | 上下文进度条格数，不设时按终端宽度在 4 到 10 之间选 |
| `display.showUsage` / `sevenDayThreshold` / `timeFormat` | 订阅额度。本周用量达到阈值才显示；时间格式 `relative`、`absolute`、`both` |
| `display.showPromptCache` / `promptCacheTtlSeconds` | 缓存剩余时间，从最后一次回复开始倒数 |
| `display.showSessionTokens` | 累计令牌行 |
| `gitStatus.showDirty` / `showAheadBehind` / `pushWarningThreshold` | 改动标记、领先落后数、领先多少提交时变色 |
| `colors.*` | 颜色名、0 到 255 色号或 `#rrggbb` |

尚未移植的 claude-hud 显示项：内存、环境（配置数量和 output style）、工具、技能、MCP、子代理、待办、会话时间、输出速度、紧凑布局（`lineLayout: compact` 按展开布局显示）。

## 命令

| 命令 | 作用 |
|---|---|
| `cchud` | 状态栏入口，由 Claude Code 调用 |
| `cchud setup` | 把状态栏指向本程序，没有配置时写入默认配置，然后自测 |
| `cchud status` | 常驻进程的 pid、版本、请求数、跟踪的会话数、内存 |
| `cchud stop` | 让常驻进程退出 |
| `cchud render < input.json` | 不经常驻进程渲染一次，用于排查 |
| `cchud version` | 版本号 |

环境变量：

| 变量 | 作用 |
|---|---|
| `CCHUD_COLUMNS` | 指定可用宽度。默认取终端宽度减 4，取不到再用 `COLUMNS`，都没有就不换行 |
| `CCHUD_CONFIG` | 指定配置文件 |
| `CCHUD_SOCKET` | 指定常驻进程的 socket 路径 |
| `CLAUDE_HUD_DISABLE=1` | 不显示状态栏 |

## 工作方式

1. Claude Code 每次刷新状态栏时启动 `cchud`。它读 stdin，通过 Unix socket 交给常驻进程，把返回的文本原样输出。连不上常驻进程时自动拉起一个；拉起失败就在本进程渲染这一次。
2. 常驻进程同一用户只有一个，空闲 30 分钟自动退出。
3. 会话记录增量读取：记住每个文件读到的位置，之后只读新追加的部分。
4. git 状态用一条 `git status --porcelain=v2 --branch` 取得，按目录缓存 2 秒，同目录的会话共用。
5. 程序文件被替换后，客户端和常驻进程的版本标识对不上，旧常驻进程自动退出。

作者机器上的实测（Apple M3 Pro，macOS 26.5，4 个会话同时开着，和 claude-hud 0.1.1 对比）：

| 项目 | claude-hud | cchud |
|---|---|---|
| 每次刷新 | 0.13 秒，内存 56MB | 不到 10 毫秒，约 6MB |
| 首次读一份 318MB 的会话记录 | 1.39 秒，167MB，之后每次有新消息都整份重读 | 0.78 秒，13MB，之后只读新增部分 |
| 常驻内存 | 无常驻进程 | 约 14MB，所有会话共用 |

## 开发

```sh
go test ./...                          # 单元测试
CCHUD_E2E=1 go test -run EndToEnd .    # 编译真实程序，测试常驻进程的拉起、响应和退出
./scripts/dev-install.sh               # 从源码安装到 ~/.local/bin/cchud
./scripts/build.sh                     # 编译 6 个平台到 dist/，附 SHA256SUMS
```

发布新版本有两种方式，结果相同：

1. 推送 `v` 开头的标签，例如 `git tag v0.1.1 && git push origin v0.1.1`。GitHub Actions 先在 Linux、macOS、Windows 上跑全部测试（包括端到端测试），通过后编译 6 个平台并发布 Release。推送到 `main` 和提交 PR 时只跑测试。
2. Actions 不可用时，在本机运行 `./scripts/release-local.sh v0.1.1`。它要求工作区干净且 `main` 已推送，跑完全部测试后编译、创建 Release 并上传文件。

## 许可

MIT，见 `LICENSE`。状态栏的布局、文字、颜色和计算方式移植自 claude-hud（Copyright (c) 2026 Jarrod Watts，MIT 许可）。
