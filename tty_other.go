//go:build !(darwin || linux || freebsd || windows)

package main

func ttyColumns() int { return 0 }
