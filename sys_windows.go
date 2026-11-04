//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

// detachAttr 让常驻进程不挂在客户端的控制台上，也不弹窗口。
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: detachedProcess | createNewProcessGroup}
}

// hideWindowAttr 用于运行 git 等子命令，避免闪出控制台窗口。
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procGetConsoleOutputCP         = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleOutputCP         = kernel32.NewProc("SetConsoleOutputCP")
)

// consoleUTF8 把控制台输出临时切到 UTF-8，返回的函数恢复原设置。
// 默认的中文代码页 936 会把程序输出的中文显示成乱码。
func consoleUTF8() func() {
	old, _, _ := procGetConsoleOutputCP.Call()
	if old == 0 || old == 65001 {
		return func() {}
	}
	procSetConsoleOutputCP.Call(65001)
	return func() { procSetConsoleOutputCP.Call(old) }
}

type consoleCoord struct{ X, Y int16 }
type consoleRect struct{ Left, Top, Right, Bottom int16 }
type consoleInfo struct {
	Size              consoleCoord
	CursorPosition    consoleCoord
	Attributes        uint16
	Window            consoleRect
	MaximumWindowSize consoleCoord
}

// ttyColumns 读当前控制台的可见宽度，读不到返回 0。
func ttyColumns() int {
	name, err := syscall.UTF16PtrFromString("CONOUT$")
	if err != nil {
		return 0
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0
	}
	defer syscall.CloseHandle(h)
	var info consoleInfo
	r, _, _ := procGetConsoleScreenBufferInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	return int(info.Window.Right-info.Window.Left) + 1
}
