//go:build darwin || linux || freebsd

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// ttyColumns 通过 /dev/tty 读终端宽度，和原命令里的 `stty size </dev/tty` 等价。读不到返回 0。
func ttyColumns() int {
	f, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return 0
	}
	defer f.Close()
	var ws struct{ Row, Col, X, Y uint16 }
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if e != 0 {
		return 0
	}
	return int(ws.Col)
}
