package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	stdinTimeout   = 2 * time.Second
	maxStdinBytes  = 1 << 20
	dialTimeout    = 300 * time.Millisecond
	requestTimeout = 3 * time.Second
	spawnWait      = 500 * time.Millisecond
)

func runClient() int {
	if hudDisabled() {
		return 0
	}
	input, ok := readStdinJSON(os.Stdin)
	if !ok {
		// Claude Code 配置检查时会不带输入运行一次。
		fmt.Println("[cchud] ready")
		return 0
	}
	req := request{Version: selfVersion(), Cols: detectWidth(), Env: collectEnv(), Input: input}
	out, err := callDaemon(req)
	if err != nil {
		// 常驻进程不可用时在本进程渲染，保证这一次状态栏照常显示。
		out = newEngine().Render(req, time.Now())
	}
	_, _ = os.Stdout.WriteString(out)
	return 0
}

func runRenderOnce() int {
	input, ok := readStdinJSON(os.Stdin)
	if !ok {
		fmt.Fprintln(os.Stderr, "cchud render: stdin 需要一份状态栏 JSON")
		return 1
	}
	req := request{Version: selfVersion(), Cols: detectWidth(), Env: collectEnv(), Input: input}
	_, _ = os.Stdout.WriteString(newEngine().Render(req, time.Now()))
	return 0
}

func runControl(cmd string) int {
	resp, err := roundTrip(request{Version: selfVersion(), Cmd: cmd}, requestTimeout)
	if err != nil {
		fmt.Println("常驻进程未运行")
		return 0
	}
	fmt.Print(resp.Out)
	return 0
}

func hudDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CLAUDE_HUD_DISABLE")))
	return v != "" && v != "0" && v != "false" && v != "off" && v != "no"
}

// readStdinJSON 读到一个完整的 JSON 值就返回，不等 stdin 关闭。
func readStdinJSON(f *os.File) (json.RawMessage, bool) {
	if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return nil, false
	}
	ch := make(chan json.RawMessage, 1)
	go func() {
		var raw json.RawMessage
		dec := json.NewDecoder(io.LimitReader(f, maxStdinBytes))
		if err := dec.Decode(&raw); err != nil {
			ch <- nil
			return
		}
		ch <- raw
	}()
	select {
	case raw := <-ch:
		if len(raw) == 0 || raw[0] != '{' {
			return nil, false
		}
		return raw, true
	case <-time.After(stdinTimeout):
		return nil, false
	}
}

func collectEnv() map[string]string {
	env := map[string]string{}
	for _, k := range forwardedEnv {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	return env
}

// detectWidth 返回可用宽度，0 表示不知道。
// 顺序：CCHUD_COLUMNS，终端实际宽度减 4（和原 statusLine 命令的算法一致），COLUMNS。
func detectWidth() int {
	if v := positiveInt(os.Getenv("CCHUD_COLUMNS")); v > 0 {
		return v
	}
	if c := ttyColumns(); c > 0 {
		if c > 4 {
			return c - 4
		}
		return 1
	}
	return positiveInt(os.Getenv("COLUMNS"))
}

func positiveInt(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

func callDaemon(req request) (string, error) {
	resp, err := roundTrip(req, requestTimeout)
	if err == nil {
		switch resp.Code {
		case codeOK:
			return resp.Out, nil
		case codeVersionMismatch:
			// 旧常驻进程已关闭监听并退出，拉起新的，这一次在本进程渲染。
			spawnDaemon()
			return "", errors.New("daemon restarting")
		default:
			return "", errors.New(resp.Out)
		}
	}
	if !isDialError(err) {
		return "", err
	}
	spawnDaemon()
	deadline := time.Now().Add(spawnWait)
	for time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		resp, err = roundTrip(req, requestTimeout)
		if err == nil && resp.Code == codeOK {
			return resp.Out, nil
		}
		if err != nil && !isDialError(err) {
			return "", err
		}
	}
	return "", errors.New("daemon unavailable")
}

func roundTrip(req request, timeout time.Duration) (response, error) {
	conn, err := net.DialTimeout("unix", socketPath(), dialTimeout)
	if err != nil {
		return response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := writeFrame(conn, req); err != nil {
		return response{}, err
	}
	var resp response
	if err := readFrame(conn, &resp, maxResponseBytes); err != nil {
		return response{}, err
	}
	return resp, nil
}

func isDialError(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

func spawnDaemon() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "daemon")
	cmd.SysProcAttr = detachAttr()
	// 工作目录放到临时目录，避免常驻进程占住某个项目目录（Windows 上会导致目录删不掉）。
	cmd.Dir = os.TempDir()
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	}
}
