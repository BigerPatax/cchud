package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestEndToEnd 编译出真实程序，验证客户端会拉起常驻进程、常驻进程能响应、stop 能让它退出。
// 会启动后台进程，所以只在 CCHUD_E2E=1 时运行（CI 在三个系统上都开着）。
func TestEndToEnd(t *testing.T) {
	if os.Getenv("CCHUD_E2E") != "1" {
		t.Skip("set CCHUD_E2E=1 to run")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "cchud")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(recommendedConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(dir, "t.jsonl")
	if err := os.WriteFile(transcript, []byte(sampleTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"CCHUD_SOCKET="+filepath.Join(dir, "s"),
		"CCHUD_CONFIG="+cfg,
		"CCHUD_COLUMNS=200",
	)
	input := `{"model":{"id":"claude-opus-5-5","display_name":"Opus 5.5"},"cwd":` + jsonString(dir) +
		`,"transcript_path":` + jsonString(transcript) +
		`,"version":"9.9.9","cost":{"total_cost_usd":2.5},"context_window":{"context_window_size":200000,"used_percentage":30,"current_usage":{"input_tokens":60000}}}`

	run := func(stdin string, args ...string) string {
		t.Helper()
		cmd := exec.Command(exe, args...)
		cmd.Env = env
		cmd.Stdin = strings.NewReader(stdin)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		return out.String()
	}
	defer run("", "stop")

	for i := 0; i < 3; i++ {
		out := stripANSI(run(input))
		for _, want := range []string{"[Opus 5.5]", "CC v9.9.9", "费用", "上下文", "30% (60k/200k)", "令牌"} {
			if !strings.Contains(out, want) {
				t.Fatalf("call %d: output missing %q:\n%s", i, want, out)
			}
		}
	}
	// 第一次调用会拉起常驻进程，稍等后 status 应该能连上，并且请求数已经累计。
	var status string
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		status = run("", "status")
		if strings.Contains(status, "pid") {
			break
		}
	}
	if !strings.Contains(status, "pid") || !strings.Contains(status, "跟踪的会话 1") {
		t.Fatalf("daemon not serving:\n%s", status)
	}
	if out := run("", "stop"); !strings.Contains(out, "已退出") {
		t.Fatalf("stop: %s", out)
	}
	stopped := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if strings.Contains(run("", "status"), "未运行") {
			stopped = true
			break
		}
	}
	if !stopped {
		t.Fatal("daemon still running 3s after stop")
	}
	// Windows 上进程完全退出后程序文件才解锁，否则临时目录删不掉。
	time.Sleep(500 * time.Millisecond)
}

func jsonString(s string) string {
	b := &strings.Builder{}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
