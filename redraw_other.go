//go:build !(darwin || linux || freebsd)

package main

// requestRedraw 在没有 SIGCONT 的平台上不做任何事。
func requestRedraw() {}
