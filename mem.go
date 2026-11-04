package main

import (
	"runtime/debug"
	"sync/atomic"
	"time"
)

var freePending atomic.Bool

// freeMemorySoon 在一次大量读取之后把空闲内存还给系统。多个请求同时触发时只做一次。
func freeMemorySoon() {
	if freePending.CompareAndSwap(false, true) {
		go func() {
			time.Sleep(200 * time.Millisecond)
			debug.FreeOSMemory()
			freePending.Store(false)
		}()
	}
}
