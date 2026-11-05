package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// recommendedConfig 是 setup 在没有任何配置时写入的默认配置，对应 README 里的截图样式。
const recommendedConfig = `{
  "language": "zh",
  "lineLayout": "expanded",
  "display": {
    "showUsage": true,
    "usageBarEnabled": true,
    "timeFormat": "both",
    "sevenDayThreshold": 0,
    "showDuration": true,
    "showTokenBreakdown": true,
    "showSessionTokens": true,
    "contextValue": "both",
    "showCost": true,
    "showClaudeCodeVersion": true,
    "showPromptCache": true,
    "promptCacheTtlSeconds": 3600,
    "contextBarWidth": 20
  },
  "gitStatus": {
    "enabled": true,
    "showDirty": true,
    "showAheadBehind": true,
    "pushWarningThreshold": 3
  },
  "colors": {
    "model": "#56B6C2",
    "project": "#E5C07B",
    "git": "#C678DD",
    "gitBranch": "#56B6C2",
    "context": "#7EC8E3",
    "usage": "#61AFEF",
    "warning": "#E5C07B",
    "usageWarning": "#C678DD",
    "critical": "#E06C75",
    "label": "#5DA2DB",
    "custom": "#D19A66"
  }
}
`

// runSetup 把状态栏指向当前程序，没有配置时写入默认配置，最后自测一次。
// 改 settings.json 之前先备份，其余设置项和顺序保持不变。
func runSetup() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法确定程序路径:", err)
		return 1
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	env := collectEnv()
	dir := claudeConfigDir(env)

	cfgPath, created, err := ensureConfig(dir, env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "写配置失败:", err)
		return 1
	}
	if created {
		fmt.Println("已写入默认配置:", cfgPath)
	} else {
		fmt.Println("沿用现有配置:", cfgPath)
	}

	settings := filepath.Join(dir, "settings.json")
	cmd := statusLineCommand(exe)
	backup, changed, err := setStatusLine(settings, cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "修改 settings.json 失败，文件未改动:", err)
		return 1
	}
	switch {
	case !changed:
		fmt.Println("settings.json 里的状态栏已经指向", cmd)
	case backup != "":
		fmt.Println("已修改", settings, "，原文件备份为", backup)
	default:
		fmt.Println("已创建", settings)
	}

	// 自测：用一份示例输入在本进程渲染一次。
	wd, _ := os.Getwd()
	sample, _ := json.Marshal(map[string]any{
		"model":          map[string]any{"id": "claude-opus-5-5", "display_name": "Opus 5.5"},
		"cwd":            wd,
		"version":        "self-test",
		"cost":           map[string]any{"total_cost_usd": 1.23},
		"context_window": map[string]any{"context_window_size": 200000, "used_percentage": 25, "current_usage": map[string]any{"input_tokens": 50000}},
	})
	out, _ := newEngine().Render(request{Cols: detectWidth(), Env: env, Input: sample}, time.Now())
	fmt.Println("自测输出:")
	fmt.Print(out)
	fmt.Println("完成。新开或继续使用 Claude Code 会话，状态栏会在下一次刷新时生效。")
	return 0
}

// ensureConfig：已有 cchud 或 claude-hud 的配置时直接用，否则写入默认配置。
func ensureConfig(dir string, env map[string]string) (string, bool, error) {
	if p := strings.TrimSpace(env["CCHUD_CONFIG"]); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, false, nil
		}
		return p, true, writeNew(p, []byte(recommendedConfig))
	}
	for _, p := range []string{ownConfigPath(dir), hudConfigPath(dir)} {
		if _, err := os.Stat(p); err == nil {
			return p, false, nil
		}
	}
	p := ownConfigPath(dir)
	return p, true, writeNew(p, []byte(recommendedConfig))
}

func writeNew(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// statusLineCommand 生成 settings.json 里的命令。Windows 上用正斜杠加引号，
// 这样在 Git Bash、cmd、PowerShell 里都能执行。
func statusLineCommand(exe string) string {
	if runtime.GOOS == "windows" {
		return `"` + filepath.ToSlash(exe) + `"`
	}
	if strings.ContainsAny(exe, " '\"$`\\!&;|<>()*?[]{}~#") {
		return "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	}
	return exe
}

type orderedField struct {
	key string
	val json.RawMessage
}

// setStatusLine 只替换顶层的 statusLine 字段。返回备份路径和是否有改动。
func setStatusLine(path, command string) (string, bool, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		Type            string `json:"type"`
		Command         string `json:"command"`
		RefreshInterval int    `json:"refreshInterval"`
	}{"command", command, 1})
	newVal := json.RawMessage(bytes.TrimSpace(buf.Bytes()))

	orig, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		out := emitOrdered([]orderedField{{"statusLine", newVal}})
		return "", true, writeNew(path, out)
	}
	if err != nil {
		return "", false, err
	}
	fields, err := parseOrdered(orig)
	if err != nil {
		return "", false, fmt.Errorf("settings.json 不是合法的 JSON 对象: %w", err)
	}
	found := false
	for i, f := range fields {
		if f.key != "statusLine" {
			continue
		}
		found = true
		var cur struct {
			Type            string `json:"type"`
			Command         string `json:"command"`
			RefreshInterval int    `json:"refreshInterval"`
		}
		if json.Unmarshal(f.val, &cur) == nil && cur.Type == "command" && cur.Command == command && cur.RefreshInterval == 1 {
			return "", false, nil
		}
		fields[i].val = newVal
	}
	if !found {
		fields = append(fields, orderedField{"statusLine", newVal})
	}
	backup := path + ".cchud-backup-" + time.Now().Format("20060102-150405")
	if err := os.WriteFile(backup, orig, 0o600); err != nil {
		return "", false, err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	// 直接写回原路径（不改名替换），settings.json 是符号链接时链接保持不变。
	if err := os.WriteFile(path, emitOrdered(fields), mode); err != nil {
		return "", false, err
	}
	return backup, true, nil
}

func parseOrdered(b []byte) ([]orderedField, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("顶层不是对象")
	}
	var fields []orderedField
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, errors.New("键不是字符串")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		fields = append(fields, orderedField{key, raw})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return fields, nil
}

func emitOrdered(fields []orderedField) []byte {
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, f := range fields {
		var kb bytes.Buffer
		enc := json.NewEncoder(&kb)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(f.key)
		var vb bytes.Buffer
		if json.Indent(&vb, f.val, "  ", "  ") != nil {
			vb.Reset()
			vb.Write(f.val)
		}
		b.WriteString("  ")
		b.Write(bytes.TrimSpace(kb.Bytes()))
		b.WriteString(": ")
		b.Write(vb.Bytes())
		if i < len(fields)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return b.Bytes()
}
