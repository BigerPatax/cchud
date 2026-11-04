package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetStatusLineKeepsOtherKeys(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	orig := `{
  "zeta": 1,
  "statusLine": {
    "type": "command",
    "command": "node old.js"
  },
  "alpha": {"a": "<b>&"},
  "hooks": []
}
`
	if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, changed, err := setStatusLine(p, "/x/cchud")
	if err != nil || !changed || backup == "" {
		t.Fatalf("changed=%v backup=%q err=%v", changed, backup, err)
	}
	got, _ := os.ReadFile(p)
	if b, _ := os.ReadFile(backup); string(b) != orig {
		t.Fatalf("backup differs from original")
	}
	// 其他键的顺序和内容不变，HTML 字符不被转义。
	s := string(got)
	if !(strings.Index(s, `"zeta"`) < strings.Index(s, `"statusLine"`) && strings.Index(s, `"statusLine"`) < strings.Index(s, `"alpha"`)) {
		t.Fatalf("key order changed:\n%s", s)
	}
	if !strings.Contains(s, `"<b>&"`) {
		t.Fatalf("value re-escaped:\n%s", s)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("output not json: %v\n%s", err, s)
	}
	sl := m["statusLine"].(map[string]any)
	if sl["command"] != "/x/cchud" || sl["refreshInterval"] != float64(1) {
		t.Fatalf("statusLine = %v", sl)
	}
	// 再运行一次不改文件。
	if _, changed, err := setStatusLine(p, "/x/cchud"); err != nil || changed {
		t.Fatalf("second run changed=%v err=%v", changed, err)
	}
}

func TestSetStatusLineCreatesAndRefusesBadJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "new", "settings.json")
	if _, changed, err := setStatusLine(p, "/x/cchud"); err != nil || !changed {
		t.Fatalf("create: changed=%v err=%v", changed, err)
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte(`{"a": 1,}`), 0o600)
	if _, _, err := setStatusLine(bad, "/x/cchud"); err == nil {
		t.Fatal("bad json accepted")
	}
	if b, _ := os.ReadFile(bad); string(b) != `{"a": 1,}` {
		t.Fatal("bad json file modified")
	}
}

func TestRecommendedConfigParses(t *testing.T) {
	cfg := defaultConfig()
	applyUserConfig(cfg, []byte(recommendedConfig))
	if cfg.Language != "zh" || !cfg.Display.ShowSessionTokens || cfg.Display.ContextBarWidth != 20 || cfg.Colors["label"] != "#5DA2DB" {
		t.Fatalf("recommended config not applied: %+v", cfg.Display)
	}
}

func TestConfigPathOrder(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"CLAUDE_CONFIG_DIR": dir}
	if got := configPath(env); got != hudConfigPath(dir) {
		t.Fatalf("without own config: %s", got)
	}
	_ = writeNew(ownConfigPath(dir), []byte("{}"))
	if got := configPath(env); got != ownConfigPath(dir) {
		t.Fatalf("with own config: %s", got)
	}
	env["CCHUD_CONFIG"] = "/explicit.json"
	if got := configPath(env); got != "/explicit.json" {
		t.Fatalf("explicit: %s", got)
	}
}
