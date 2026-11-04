// cchud 是 Claude Code 状态栏程序，显示内容和样式移植自 claude-hud（MIT，jarrodwatts/claude-hud）。
//
// 运行方式：
//
//	cchud          客户端。Claude Code 每次刷新状态栏时调用，读 stdin，交给常驻进程渲染。
//	cchud daemon   常驻进程。所有会话共用一个，由客户端自动拉起，空闲 30 分钟退出。
//	cchud render   不经常驻进程，直接在本进程渲染一次。用于排查问题。
//	cchud status   显示常驻进程的状态。
//	cchud stop     让常驻进程退出。
package main

import (
	"fmt"
	"os"
)

// buildVersion 由构建脚本用 -ldflags 写入。客户端和常驻进程版本不一致时，旧常驻进程退出。
var buildVersion = "dev"

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "":
		os.Exit(runClient())
	case "daemon":
		os.Exit(runDaemon())
	case "render":
		os.Exit(runRenderOnce())
	case "status", "stop":
		os.Exit(runControl(cmd))
	case "version", "-v", "--version":
		fmt.Println("cchud", buildVersion)
	default:
		fmt.Fprintln(os.Stderr, "用法: cchud [daemon|render|status|stop|version]")
		os.Exit(2)
	}
}
