//go:build darwin || linux || freebsd

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// requestRedraw 让 Claude Code 整屏重画一次。
//
// Claude Code 收到 SIGCONT 时按“从 Ctrl+Z 挂起恢复”处理：丢掉上一帧，把整个界面重新写一遍。
// 进程没有被挂起时，SIGCONT 不会改变它的运行状态。
// 被挂起的进程不发，否则会让它恢复运行。
func requestRedraw() {
	pid := claudePID()
	if pid > 1 {
		_ = syscall.Kill(pid, syscall.SIGCONT)
	}
}

// claudePID 返回启动状态栏命令的 Claude Code 进程，找不到返回 0。
// Claude Code 在新的会话里运行状态栏命令，这个命令和中间的 shell 都没有控制终端。
// 沿父进程往上找，第一个处在终端前台（ps 状态带 +）的祖先就是 Claude Code。
// 只在要重画时调用，每次压缩对话后最多一次，所以直接运行 ps。
func claudePID() int {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,stat=").Output()
	if err != nil {
		return 0
	}
	type proc struct {
		ppid int
		stat string
	}
	procs := map[int]proc{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			procs[pid] = proc{ppid, f[2]}
		}
	}
	pid := os.Getppid()
	for i := 0; i < 8 && pid > 1; i++ {
		p, ok := procs[pid]
		if !ok {
			return 0
		}
		if strings.Contains(p.stat, "+") {
			if strings.Contains(p.stat, "T") {
				return 0
			}
			return pid
		}
		pid = p.ppid
	}
	return 0
}
