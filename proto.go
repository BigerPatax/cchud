package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
)

const (
	codeOK              = 0
	codeError           = 1
	codeVersionMismatch = 2

	maxRequestBytes  = 2 << 20
	maxResponseBytes = 4 << 20
)

// request 是客户端发给常驻进程的一次请求。
type request struct {
	Version string            `json:"v"`
	Cmd     string            `json:"cmd,omitempty"`
	Cols    int               `json:"cols"` // 0 表示不知道终端宽度
	Env     map[string]string `json:"env,omitempty"`
	Input   json.RawMessage   `json:"input,omitempty"`
}

type response struct {
	Code   int    `json:"code"`
	Out    string `json:"out"`
	Redraw bool   `json:"redraw,omitempty"` // 客户端输出后让 Claude Code 整屏重画一次
}

// 转发给常驻进程的环境变量。常驻进程被多个会话共用，不能用它自己启动时的环境。
var forwardedEnv = []string{"CLAUDE_CONFIG_DIR", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CCHUD_CONFIG"}

var errFrameTooLarge = errors.New("frame too large")

// 帧格式：4 字节大端长度，后跟 JSON。
func writeFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err = w.Write(buf)
	return err
}

func readFrame(r io.Reader, v any, max int) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := int(binary.BigEndian.Uint32(hdr[:]))
	if n > max {
		return errFrameTooLarge
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// socketPath 返回本用户的 Unix socket 路径。Windows 10 1803 起也支持 Unix socket。
func socketPath() string {
	if p := os.Getenv("CCHUD_SOCKET"); p != "" {
		return p
	}
	dir := os.TempDir()
	if runtime.GOOS == "linux" {
		if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
			dir = x
		}
	}
	return filepath.Join(dir, "cchud-"+userTag()+".sock")
}

func userTag() string {
	if uid := os.Getuid(); uid >= 0 {
		return strconv.Itoa(uid)
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return unsafeNameChars.ReplaceAllString(u, "_")
	}
	return "user"
}

// selfVersion 带上程序文件的修改时间和大小。替换程序文件后，新客户端和旧常驻进程的版本就对不上。
func selfVersion() string {
	exe, err := os.Executable()
	if err != nil {
		return buildVersion
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return buildVersion
	}
	return buildVersion + ":" + strconv.FormatInt(fi.ModTime().UnixNano(), 36) + ":" + strconv.FormatInt(fi.Size(), 36)
}
