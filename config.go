package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config 对应 claude-hud 的 config.json，只保留 cchud 用到的字段，默认值和 claude-hud 0.1.1 一致。
type Config struct {
	Language      string
	LineLayout    string
	PathLevels    int
	MaxWidth      int // 0 表示没设
	ForceMaxWidth bool
	// RedrawAfterCompact 为 true 时，压缩对话后让 Claude Code 整屏重画一次，见 requestRedraw。
	RedrawAfterCompact bool
	ElementOrder       []string
	Git                GitConfig
	Display            DisplayConfig
	Colors             map[string]any
}

type GitConfig struct {
	Enabled               bool
	ShowDirty             bool
	ShowAheadBehind       bool
	BranchOverflow        string
	PushWarningThreshold  int
	PushCriticalThreshold int
}

type DisplayConfig struct {
	ShowModel                bool
	ShowProject              bool
	ShowAddedDirs            bool
	AddedDirsLayout          string
	ShowContextBar           bool
	ContextValue             string
	ContextBarWidth          int // 0 表示按终端宽度自适应
	ShowCost                 bool
	ShowDuration             bool
	ShowTokenBreakdown       bool
	ShowUsage                bool
	UsageValue               string
	UsageBarEnabled          bool
	ShowResetLabel           bool
	UsageCompact             bool
	ShowClaudeCodeVersion    bool
	ShowPromptCache          bool
	PromptCacheTTLSeconds    int
	ShowSessionTokens        bool
	MergeGroups              [][]string
	AutocompactBuffer        string
	ContextWarningThreshold  int
	ContextCriticalThreshold int
	UsageThreshold           int
	SevenDayThreshold        int
	ModelFormat              string
	ModelOverride            string
	CustomLine               string
	CustomLinePosition       string
	TimeFormat               string
	AutoCompactWindow        int // 0 表示没设
}

var defaultElementOrder = []string{
	"project", "addedDirs", "context", "usage", "promptCache", "memory", "environment",
	"tools", "skills", "mcp", "agents", "todos", "sessionTime",
}

var knownElements = func() map[string]bool {
	m := map[string]bool{}
	for _, e := range defaultElementOrder {
		m[e] = true
	}
	return m
}()

func defaultConfig() *Config {
	return &Config{
		Language:           "en",
		LineLayout:         "expanded",
		PathLevels:         1,
		RedrawAfterCompact: true,
		ElementOrder:       append([]string(nil), defaultElementOrder...),
		Git: GitConfig{
			Enabled: true, ShowDirty: true, BranchOverflow: "truncate",
		},
		Display: DisplayConfig{
			ShowModel: true, ShowProject: true, ShowAddedDirs: true, AddedDirsLayout: "inline",
			ShowContextBar: true, ContextValue: "percent", ShowTokenBreakdown: true,
			ShowUsage: true, UsageValue: "percent", UsageBarEnabled: true, ShowResetLabel: true,
			PromptCacheTTLSeconds:   300,
			MergeGroups:             [][]string{{"context", "usage"}},
			AutocompactBuffer:       "enabled",
			ContextWarningThreshold: 70, ContextCriticalThreshold: 85,
			SevenDayThreshold: 80,
			ModelFormat:       "full", CustomLinePosition: "last", TimeFormat: "relative",
		},
		Colors: map[string]any{
			"context": "green", "usage": "brightBlue", "warning": "yellow", "usageWarning": "brightMagenta",
			"critical": "red", "model": "cyan", "project": "yellow", "git": "magenta", "gitBranch": "cyan",
			"label": "dim", "custom": float64(208), "barFilled": "█", "barEmpty": "░",
		},
	}
}

// claudeConfigDir 和 claude-hud 的 getClaudeConfigDir 一致：优先 CLAUDE_CONFIG_DIR，否则 ~/.claude。
func claudeConfigDir(env map[string]string) string {
	home, _ := os.UserHomeDir()
	if v := strings.TrimSpace(env["CLAUDE_CONFIG_DIR"]); v != "" {
		if v == "~" {
			return home
		}
		if strings.HasPrefix(v, "~/") || strings.HasPrefix(v, `~\`) {
			return filepath.Join(home, v[2:])
		}
		if abs, err := filepath.Abs(v); err == nil {
			return abs
		}
		return v
	}
	return filepath.Join(home, ".claude")
}

// configPath 的顺序：CCHUD_CONFIG，~/.claude/cchud/config.json，claude-hud 的 config.json。
// CCHUD_CONFIG 取自请求里的环境变量，因为常驻进程被多个会话共用。
func configPath(env map[string]string) string {
	if p := strings.TrimSpace(env["CCHUD_CONFIG"]); p != "" {
		return p
	}
	dir := claudeConfigDir(env)
	own := ownConfigPath(dir)
	if _, err := os.Stat(own); err == nil {
		return own
	}
	return hudConfigPath(dir)
}

func ownConfigPath(claudeDir string) string { return filepath.Join(claudeDir, "cchud", "config.json") }

func hudConfigPath(claudeDir string) string {
	return filepath.Join(claudeDir, "plugins", "claude-hud", "config.json")
}

// configCache 按文件修改时间缓存解析结果，文件没变就不重读。
type configCache struct {
	mu      sync.Mutex
	entries map[string]*configEntry
}

type configEntry struct {
	mtime   time.Time
	size    int64
	cfg     *Config
	checked time.Time
}

func newConfigCache() *configCache { return &configCache{entries: map[string]*configEntry{}} }

func (c *configCache) load(path string, now time.Time) *Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[path]
	// 同一秒内的多次刷新不重复 stat。
	if e != nil && now.Sub(e.checked) < time.Second {
		return e.cfg
	}
	fi, err := os.Stat(path)
	if err != nil {
		cfg := defaultConfig()
		c.entries[path] = &configEntry{cfg: cfg, checked: now}
		return cfg
	}
	if e != nil && e.mtime.Equal(fi.ModTime()) && e.size == fi.Size() {
		e.checked = now
		return e.cfg
	}
	cfg := defaultConfig()
	if b, err := os.ReadFile(path); err == nil {
		applyUserConfig(cfg, b)
	}
	c.entries[path] = &configEntry{mtime: fi.ModTime(), size: fi.Size(), cfg: cfg, checked: now}
	return cfg
}

// applyUserConfig 把用户配置合并进默认值。类型不对或取值不合法的字段保留默认值。
func applyUserConfig(cfg *Config, b []byte) {
	var root map[string]json.RawMessage
	if json.Unmarshal(lenientJSON(b), &root) != nil {
		return
	}
	setEnum(root, "language", &cfg.Language, "en", "zh", "zh-Hans")
	setEnum(root, "lineLayout", &cfg.LineLayout, "compact", "expanded")
	if v, ok := getInt(root, "pathLevels"); ok && v >= 1 && v <= 3 {
		cfg.PathLevels = v
	}
	if v, ok := getInt(root, "maxWidth"); ok && v > 0 {
		cfg.MaxWidth = v
	}
	setBool(root, "forceMaxWidth", &cfg.ForceMaxWidth)
	setBool(root, "redrawAfterCompact", &cfg.RedrawAfterCompact)
	if raw, ok := root["elementOrder"]; ok {
		var order []string
		if json.Unmarshal(raw, &order) == nil {
			seen := map[string]bool{}
			var valid []string
			for _, e := range order {
				if knownElements[e] && !seen[e] {
					seen[e] = true
					valid = append(valid, e)
				}
			}
			if len(valid) > 0 {
				cfg.ElementOrder = valid
			}
		}
	}

	if g := section(root, "gitStatus"); g != nil {
		setBool(g, "enabled", &cfg.Git.Enabled)
		setBool(g, "showDirty", &cfg.Git.ShowDirty)
		setBool(g, "showAheadBehind", &cfg.Git.ShowAheadBehind)
		setEnum(g, "branchOverflow", &cfg.Git.BranchOverflow, "truncate", "wrap")
		setNonNegInt(g, "pushWarningThreshold", &cfg.Git.PushWarningThreshold)
		setNonNegInt(g, "pushCriticalThreshold", &cfg.Git.PushCriticalThreshold)
	}

	if d := section(root, "display"); d != nil {
		dc := &cfg.Display
		setBool(d, "showModel", &dc.ShowModel)
		setBool(d, "showProject", &dc.ShowProject)
		setBool(d, "showAddedDirs", &dc.ShowAddedDirs)
		setEnum(d, "addedDirsLayout", &dc.AddedDirsLayout, "inline", "line")
		setBool(d, "showContextBar", &dc.ShowContextBar)
		setEnum(d, "contextValue", &dc.ContextValue, "percent", "tokens", "remaining", "both")
		if v, ok := getInt(d, "contextBarWidth"); ok && v > 0 {
			dc.ContextBarWidth = v
		}
		setBool(d, "showCost", &dc.ShowCost)
		setBool(d, "showDuration", &dc.ShowDuration)
		setBool(d, "showTokenBreakdown", &dc.ShowTokenBreakdown)
		setBool(d, "showUsage", &dc.ShowUsage)
		setEnum(d, "usageValue", &dc.UsageValue, "percent", "remaining")
		setBool(d, "usageBarEnabled", &dc.UsageBarEnabled)
		setBool(d, "showResetLabel", &dc.ShowResetLabel)
		setBool(d, "usageCompact", &dc.UsageCompact)
		setBool(d, "showClaudeCodeVersion", &dc.ShowClaudeCodeVersion)
		setBool(d, "showPromptCache", &dc.ShowPromptCache)
		if v, ok := getInt(d, "promptCacheTtlSeconds"); ok && v > 0 {
			dc.PromptCacheTTLSeconds = v
		}
		setBool(d, "showSessionTokens", &dc.ShowSessionTokens)
		if raw, ok := d["mergeGroups"]; ok {
			var groups [][]string
			if json.Unmarshal(raw, &groups) == nil {
				dc.MergeGroups = groups
			}
		}
		setEnum(d, "autocompactBuffer", &dc.AutocompactBuffer, "enabled", "disabled")
		setThreshold(d, "contextWarningThreshold", &dc.ContextWarningThreshold)
		setThreshold(d, "contextCriticalThreshold", &dc.ContextCriticalThreshold)
		setThreshold(d, "usageThreshold", &dc.UsageThreshold)
		setThreshold(d, "sevenDayThreshold", &dc.SevenDayThreshold)
		setEnum(d, "modelFormat", &dc.ModelFormat, "full", "compact", "short")
		setString(d, "modelOverride", &dc.ModelOverride)
		setString(d, "customLine", &dc.CustomLine)
		setEnum(d, "customLinePosition", &dc.CustomLinePosition, "first", "last")
		setEnum(d, "timeFormat", &dc.TimeFormat, "relative", "absolute", "both", "elapsed", "elapsedAndAbsolute")
		if v, ok := getInt(d, "autoCompactWindow"); ok && v > 0 {
			dc.AutoCompactWindow = v
		}
	}

	if c := section(root, "colors"); c != nil {
		for k, raw := range c {
			var v any
			if json.Unmarshal(raw, &v) != nil {
				continue
			}
			switch k {
			case "barFilled", "barEmpty":
				if s, ok := v.(string); ok && len([]rune(s)) == 1 {
					cfg.Colors[k] = s
				}
			default:
				if validColor(v) {
					cfg.Colors[k] = v
				}
			}
		}
	}
}

func section(m map[string]json.RawMessage, key string) map[string]json.RawMessage {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var sub map[string]json.RawMessage
	if json.Unmarshal(raw, &sub) != nil {
		return nil
	}
	return sub
}

func setBool(m map[string]json.RawMessage, key string, dst *bool) {
	if raw, ok := m[key]; ok {
		var v bool
		if json.Unmarshal(raw, &v) == nil {
			*dst = v
		}
	}
}

func setString(m map[string]json.RawMessage, key string, dst *string) {
	if raw, ok := m[key]; ok {
		var v string
		if json.Unmarshal(raw, &v) == nil {
			*dst = v
		}
	}
}

func setEnum(m map[string]json.RawMessage, key string, dst *string, allowed ...string) {
	if raw, ok := m[key]; ok {
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return
		}
		for _, a := range allowed {
			if v == a {
				*dst = v
				return
			}
		}
	}
}

func getInt(m map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return int(math.Floor(f)), true
}

func setNonNegInt(m map[string]json.RawMessage, key string, dst *int) {
	if v, ok := getInt(m, key); ok && v >= 0 {
		*dst = v
	}
}

func setThreshold(m map[string]json.RawMessage, key string, dst *int) {
	if v, ok := getInt(m, key); ok && v >= 0 && v <= 100 {
		*dst = v
	}
}

func validColor(v any) bool {
	switch x := v.(type) {
	case float64:
		return x >= 0 && x <= 255 && x == math.Trunc(x)
	case string:
		if strings.HasPrefix(x, "#") {
			if len(x) != 7 {
				return false
			}
			for _, ch := range x[1:] {
				if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
					return false
				}
			}
			return true
		}
		_, ok := namedColors[x]
		return ok
	}
	return false
}

// lenientJSON 去掉字符串外的 // 和 /* */ 注释，以及 } 或 ] 前多余的逗号。
// 手改配置时常留下这类错误，严格解析会让整份配置失效。
func lenientJSON(b []byte) []byte {
	out := make([]byte, 0, len(b))
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out = append(out, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out = append(out, c)
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			if i < len(b) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			i++
		case c == '}' || c == ']':
			j := len(out) - 1
			for j >= 0 && (out[j] == ' ' || out[j] == '\t' || out[j] == '\n' || out[j] == '\r') {
				j--
			}
			if j >= 0 && out[j] == ',' {
				out = append(out[:j], out[j+1:]...)
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}
