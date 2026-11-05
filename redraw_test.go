package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTakeRedraw(t *testing.T) {
	s := newTranscriptState("")
	c := time.Date(2026, 11, 5, 5, 2, 35, 0, time.UTC)
	s.data.LastCompactAt = c
	steps := []struct {
		at   time.Duration
		want bool
	}{
		{time.Second, false},     // Claude Code 可能还没画完，先不重画
		{3 * time.Second, true},  // 到时间，重画一次
		{4 * time.Second, false}, // 同一次压缩只重画一次
	}
	for _, st := range steps {
		if got := s.takeRedraw(c.Add(st.at)); got != st.want {
			t.Fatalf("at +%v: got %v, want %v", st.at, got, st.want)
		}
	}
	c2 := c.Add(time.Hour)
	s.data.LastCompactAt = c2
	if !s.takeRedraw(c2.Add(5 * time.Second)) {
		t.Fatal("second compaction not redrawn")
	}

	// 常驻进程重启后读到的旧压缩记录不触发重画，之后也不会再触发。
	old := newTranscriptState("")
	now := c.Add(10 * time.Minute)
	old.data.LastCompactAt = c
	if old.takeRedraw(now) || old.takeRedraw(now.Add(time.Second)) {
		t.Fatal("old compaction triggered a redraw")
	}
}

// 会话记录里新追加一条压缩记录，3 秒后的那次刷新带上重画标记，只带一次。
func TestRenderRedrawAfterCompact(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	writeFile(t, cfgPath, `{"gitStatus": {"enabled": false}}`)
	tp := filepath.Join(dir, "t.jsonl")
	writeFile(t, tp, `{"type":"user","timestamp":"2026-11-05T01:00:00.000Z"}`+"\n")
	input, _ := json.Marshal(map[string]any{"transcript_path": tp, "cwd": dir})
	req := request{Env: map[string]string{"CCHUD_CONFIG": cfgPath}, Input: input}

	e := newEngine()
	now := time.Now()
	if _, redraw := e.Render(req, now); redraw {
		t.Fatal("redraw without compaction")
	}
	compactAt := now.Add(-4 * time.Second).UTC().Format(time.RFC3339Nano)
	appendFile(t, tp, fmt.Sprintf(`{"type":"system","subtype":"compact_boundary","timestamp":%q,"compactMetadata":{"postTokens":25477}}`+"\n", compactAt))
	if _, redraw := e.Render(req, now); !redraw {
		t.Fatal("no redraw after compaction")
	}
	if _, redraw := e.Render(req, now.Add(time.Second)); redraw {
		t.Fatal("redraw requested twice for one compaction")
	}

	// 配置里关掉以后不重画。
	writeFile(t, cfgPath, `{"gitStatus": {"enabled": false}, "redrawAfterCompact": false}`)
	if _, redraw := newEngine().Render(req, now); redraw {
		t.Fatal("redraw while disabled")
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}
