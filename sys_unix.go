//go:build unix

package main

import "syscall"

// detachAttr 让常驻进程脱离客户端的会话，客户端退出后它继续运行。
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// hideWindowAttr 只在 Windows 上有作用。
func hideWindowAttr() *syscall.SysProcAttr { return nil }

// consoleUTF8 只在 Windows 上有作用。
func consoleUTF8() func() { return func() {} }
