package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleTranscript = `{"type":"summary","summary":"x"}
{"type":"user","timestamp":"2026-11-04T01:00:00.000Z","message":{"role":"user","content":"hi \"type\":\"assistant\""}}
{"type":"assistant","timestamp":"2026-11-04T01:00:05.000Z","message":{"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000}}}
{"type":"assistant","timestamp":"2026-11-04T01:00:05.500Z","message":{"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000}}}

{"type":"assistant","timestamp":"2026-11-04T01:00:06.000Z","message":{"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000}}}
not json "type":"assistant"
{"type":"system","subtype":"compact_boundary","timestamp":"2026-11-04T01:10:00.000Z","compactMetadata":{"postTokens":4200}}
{"type":"assistant","timestamp":"2026-11-04T01:11:00.000Z","message":{"usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3}}}
`

// 连续两条相同用量只算一次；空行打断连续，第三条再算一次。
func TestTranscriptCounting(t *testing.T) {
	p := writeTemp(t, sampleTranscript)
	s := newTranscriptState(p)
	s.refresh()
	want := SessionTokens{Input: 21, Output: 12, CacheCreation: 200, CacheRead: 2003}
	if s.data.Tokens != want {
		t.Fatalf("tokens = %+v, want %+v", s.data.Tokens, want)
	}
	if got := s.data.SessionStart.Format(time.RFC3339); got != "2026-11-04T01:00:00Z" {
		t.Fatalf("session start = %s", got)
	}
	if got := s.data.LastAssistantAt.Format(time.RFC3339); got != "2026-11-04T01:11:00Z" {
		t.Fatalf("last assistant = %s", got)
	}
	if s.data.LastCompactPost != 4200 {
		t.Fatalf("compact post = %d", s.data.LastCompactPost)
	}
}

// 文件分多次写入（包括一行只写了一半）时，增量读的结果要和一次读完相同。
func TestTranscriptIncrementalMatchesFull(t *testing.T) {
	src := sampleTranscript
	if p := os.Getenv("CCHUD_TEST_TRANSCRIPT"); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		src = string(b)
	}
	full := newTranscriptState(writeTemp(t, src))
	full.refresh()
	if strings.HasSuffix(src, "\n") && full.offset != int64(len(src)) {
		t.Fatalf("full read offset %d, file size %d", full.offset, len(src))
	}

	p := filepath.Join(t.TempDir(), "inc.jsonl")
	inc := newTranscriptState(p)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	step := len(src)/37 + 1
	var read int64
	for i := 0; i < len(src); i += step {
		end := min(i+step, len(src))
		if _, err := f.WriteString(src[i:end]); err != nil {
			t.Fatal(err)
		}
		read += inc.refresh()
	}
	read += inc.refresh()
	// 累计读取量等于文件大小，说明每次只读了新增部分，没有从头重读。
	if read != inc.offset {
		t.Fatalf("read %d bytes in total, final offset %d", read, inc.offset)
	}
	if inc.data != full.data {
		t.Fatalf("incremental %+v\nfull %+v", inc.data, full.data)
	}
	if inc.offset != full.offset {
		t.Fatalf("offset %d vs %d", inc.offset, full.offset)
	}
}

// 文件被截短或替换后从头重算。
func TestTranscriptTruncateResets(t *testing.T) {
	p := writeTemp(t, sampleTranscript)
	s := newTranscriptState(p)
	s.refresh()
	short := strings.SplitN(sampleTranscript, "\n", 4)
	if err := os.WriteFile(p, []byte(strings.Join(short[:3], "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.refresh()
	want := SessionTokens{Input: 10, Output: 5, CacheCreation: 100, CacheRead: 1000}
	if s.data.Tokens != want {
		t.Fatalf("after truncate tokens = %+v, want %+v", s.data.Tokens, want)
	}
}

func TestContextFallback(t *testing.T) {
	s := newTranscriptState(writeTemp(t, ""))
	now := time.Now()
	size, used := 1000000.0, 40.0
	in := 400000.0
	good := &ContextWindow{ContextWindowSize: &size, UsedPercentage: &used, CurrentUsage: &Usage{InputTokens: &in}}
	s.applyContextFallback(good, now)

	zero := 0.0
	glitch := &ContextWindow{ContextWindowSize: &size, UsedPercentage: &zero}
	s.applyContextFallback(glitch, now.Add(time.Second))
	if num(glitch.UsedPercentage) != 40 {
		t.Fatalf("glitch frame not restored: %v", num(glitch.UsedPercentage))
	}

	// 压缩之后的 0 是真实的，不能用压缩前的快照覆盖。
	s.data.LastCompactAt = now.Add(2 * time.Second)
	s.data.LastCompactPost = 50000
	after := &ContextWindow{ContextWindowSize: &size, UsedPercentage: &zero}
	s.applyContextFallback(after, now.Add(3*time.Second))
	if num(after.UsedPercentage) != 5 {
		t.Fatalf("post compact percent = %v, want 5", num(after.UsedPercentage))
	}
}

func TestLenientJSON(t *testing.T) {
	cfg := defaultConfig()
	applyUserConfig(cfg, []byte(`{
  // 注释
  "language": "zh", /* 块注释 */
  "display": {"showCost": true, "contextBarWidth": 20,},
  "colors": {"label": "#5DA2DB", "model": "nope", "url": "http://a//b",},
}`))
	if cfg.Language != "zh" || !cfg.Display.ShowCost || cfg.Display.ContextBarWidth != 20 {
		t.Fatalf("config not applied: %+v", cfg)
	}
	if cfg.Colors["label"] != "#5DA2DB" || cfg.Colors["model"] != "cyan" {
		t.Fatalf("colors: %v", cfg.Colors)
	}
}

func TestFormatTokens(t *testing.T) {
	cases := map[int64]string{0: "0", 999: "999", 1000: "1k", 1500: "2k", 554000: "554k",
		1_000_000: "1.0M", 1_250_000: "1.3M", 98_100_000: "98.1M"}
	for n, want := range cases {
		if got := formatTokens(n); got != want {
			t.Errorf("formatTokens(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestWidth(t *testing.T) {
	cases := []struct {
		s    string
		cjk  bool
		want int
	}{
		{"abc", false, 3}, {"上下文", false, 6}, {"♻️ 1m", false, 5}, {"⏱️  1h", false, 6},
		{"██░", false, 3}, {"██░", true, 6}, {"│", true, 2}, {"👨‍👩‍👧", false, 2},
	}
	for _, c := range cases {
		if got := textWidth(c.s, c.cjk); got != c.want {
			t.Errorf("textWidth(%q, %v) = %d, want %d", c.s, c.cjk, got, c.want)
		}
	}
	linked := hyperlink("file:///x", paint("项目", ansiYellow))
	if got := visibleWidth(linked, true); got != 4 {
		t.Errorf("visibleWidth(link) = %d", got)
	}
}

func TestGitStatusParse(t *testing.T) {
	if got := githubBranchURL("git@github.com:a/b.git", "feat/x y"); got != "https://github.com/a/b/tree/feat%2Fx%20y" {
		t.Errorf("ssh url: %s", got)
	}
	if got := githubBranchURL("https://gitlab.com/a/b.git", "main"); got != "" {
		t.Errorf("non github: %s", got)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
