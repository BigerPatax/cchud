# cchud

Claude Code 状态栏程序。显示内容和样式移植自 [claude-hud](https://github.com/jarrodwatts/claude-hud)（MIT 许可）0.1.1 构建的展开布局。目的是不再每秒为每个会话启动一个 Node 进程。

支持 macOS、Linux、Windows（Windows 10 1803 及以上，需要系统支持 Unix socket）。

## 工作方式

一个程序，两种运行方式：

1. **客户端**：Claude Code 每次刷新状态栏时启动它。它读 stdin，通过 Unix socket 交给常驻进程，把返回的文本原样输出。连不上常驻进程时自动拉起一个；拉起失败就在本进程渲染这一次。
2. **常驻进程**：同一用户的所有会话共用一个。空闲 30 分钟自动退出。

常驻进程省资源的做法：

1. 会话记录增量读取。记住每个文件读到的位置，之后只读新追加的部分。文件变短或被替换时从头读。
2. 只完整解析可能是 assistant 记录或压缩边界的行，其余行跳过。
3. git 状态用一条 `git status --porcelain=v2 --branch` 取得，按目录缓存 2 秒，同目录的会话共用。缓存过期后先返回旧值，后台刷新。
4. Claude Code 版本号直接取 stdin 里的 `version` 字段。
5. 配置文件按修改时间缓存。

替换程序文件后，客户端和常驻进程的版本标识（构建版本、文件修改时间、文件大小）对不上，旧常驻进程自动退出，下一次刷新换成新版本。

## 配置

沿用 claude-hud 的配置文件 `~/.claude/plugins/claude-hud/config.json`（设置了 `CLAUDE_CONFIG_DIR` 时用它下面的同名路径）。允许注释和多余的逗号。也可以用环境变量 `CCHUD_CONFIG` 指定别的路径。

已移植的显示项：

1. 第一行：模型、项目、git 分支（改动标记、领先落后数）、CC 版本、会话时长、费用
2. 上下文进度条（`contextValue`、`contextBarWidth`、告警阈值、接近上限时的令牌明细）
3. 订阅额度（5 小时窗口和本周，`rate_limits` 有数据时显示，和上下文合并为一行）
4. 提示词缓存倒计时（`showPromptCache`、`promptCacheTtlSeconds`）
5. 会话令牌累计（`showSessionTokens`）
6. `/add-dir` 加入的目录、`customLine`、`elementOrder`、`mergeGroups`、`colors`

未移植：内存、环境（配置数量和 output style）、工具、技能、MCP、子代理、待办、会话时间、输出速度、紧凑布局（`lineLayout: compact` 按展开布局显示）。

## 安装

macOS 或 Linux：

```sh
./install.sh            # 安装到 ~/.local/bin/cchud
```

`~/.claude/settings.json`：

```json
"statusLine": {
  "type": "command",
  "command": "~/.local/bin/cchud",
  "refreshInterval": 1
}
```

Windows：在任意一台机器上运行 `./build.sh`，把 `dist/cchud-windows-amd64.exe`（ARM 机器用 `arm64`）复制到 `%USERPROFILE%\.local\bin\cchud.exe`，状态栏命令写这个程序的路径，写法和原来的 node 命令一致。

## 命令

```sh
cchud status     # 常驻进程的 pid、版本、请求数、跟踪的会话数、内存
cchud stop       # 让常驻进程退出
cchud render < input.json   # 不经常驻进程渲染一次，用于排查
```

环境变量：

| 变量 | 作用 |
|---|---|
| `CCHUD_COLUMNS` | 指定可用宽度，优先于自动检测 |
| `CCHUD_SOCKET` | 指定 socket 路径 |
| `CCHUD_CONFIG` | 指定配置文件 |
| `CLAUDE_HUD_DISABLE=1` | 不显示状态栏 |

宽度检测顺序：`CCHUD_COLUMNS`，终端实际宽度减 4，`COLUMNS`。都拿不到时不换行。

## 测试

```sh
go test ./...
CCHUD_TEST_TRANSCRIPT=/path/to/session.jsonl go test -run Incremental .   # 用真实会话记录验证增量读取
```
