package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strconv"
	"sync"
	"time"
)

// SessionTokens 是整个会话累计的令牌数。
type SessionTokens struct {
	Input, Output, CacheCreation, CacheRead int64
}

func (t SessionTokens) total() int64 { return t.Input + t.Output + t.CacheCreation + t.CacheRead }

// TranscriptData 是渲染需要的会话记录汇总。
type TranscriptData struct {
	Valid           bool // 会话记录文件存在且读过
	SessionStart    time.Time
	LastAssistantAt time.Time
	Tokens          SessionTokens
	LastCompactAt   time.Time
	LastCompactPost int64 // -1 表示没有
}

// ctxSnapshot 是最近一次正常的上下文数据。Claude Code 偶尔会发全 0 的上下文，这时用它补上。
type ctxSnapshot struct {
	used      float64
	remaining *float64
	usage     *Usage
	size      *float64
	savedAt   time.Time
}

// transcriptState 记住一个会话记录文件读到哪里，之后只读新追加的部分。
type transcriptState struct {
	mu           sync.Mutex
	path         string
	info         os.FileInfo
	offset       int64
	lastUsageKey string
	data         TranscriptData
	snap         *ctxSnapshot
	lastUsed     time.Time
}

var (
	markAssistant = []byte(`"type":"assistant"`)
	markCompact   = []byte(`"compact_boundary"`)
	markTimestamp = []byte(`"timestamp"`)
)

func newTranscriptState(path string) *transcriptState {
	s := &transcriptState{path: path}
	s.reset()
	return s
}

func (s *transcriptState) reset() {
	s.info = nil
	s.offset = 0
	s.lastUsageKey = ""
	s.data = TranscriptData{LastCompactPost: -1}
}

// refresh 读入文件新增的完整行，返回本次读了多少字节。
// 文件变短或被替换时从头读。最后一行没有换行符时先不读，等它写完。
func (s *transcriptState) refresh() int64 {
	fi, err := os.Stat(s.path)
	if err != nil || !fi.Mode().IsRegular() {
		s.reset()
		return 0
	}
	if s.info != nil && (!os.SameFile(s.info, fi) || fi.Size() < s.offset) {
		s.reset()
	}
	s.info = fi
	s.data.Valid = true
	if fi.Size() == s.offset {
		return 0
	}
	f, err := os.Open(s.path)
	if err != nil {
		return 0
	}
	defer f.Close()
	if _, err := f.Seek(s.offset, io.SeekStart); err != nil {
		return 0
	}
	start := s.offset
	r := bufio.NewReaderSize(f, 256<<10)
	var long []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			long = append(long, chunk...)
			continue
		}
		if err != nil {
			break
		}
		line := chunk
		if len(long) > 0 {
			long = append(long, chunk...)
			line = long
		}
		s.offset += int64(len(line))
		s.processLine(line[:len(line)-1])
		long = long[:0]
	}
	return s.offset - start
}

type tUsage struct {
	InputTokens              *float64 `json:"input_tokens"`
	OutputTokens             *float64 `json:"output_tokens"`
	CacheCreationInputTokens *float64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *float64 `json:"cache_read_input_tokens"`
}

type tEntry struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	Timestamp       string          `json:"timestamp"`
	Message         json.RawMessage `json:"message"`
	CompactMetadata json.RawMessage `json:"compactMetadata"`
}

// processLine 的计数规则和 claude-hud 的 parseTranscript 一致：
// Claude Code 会把同一次 API 返回连续写两三遍，连续两条用量完全相同的 assistant 记录只算一次。
// 空行、解析失败的行、不带用量的记录都会打断这种连续。
func (s *transcriptState) processLine(line []byte) {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(bytes.TrimSpace(line)) == 0 {
		s.lastUsageKey = ""
		return
	}
	// 只有可能是 assistant 记录或压缩边界的行才完整解析，其余行（大部分是工具输出）跳过。
	if !bytes.Contains(line, markAssistant) && !bytes.Contains(line, markCompact) {
		s.lastUsageKey = ""
		if s.data.SessionStart.IsZero() && bytes.Contains(line, markTimestamp) {
			var e struct {
				Timestamp string `json:"timestamp"`
			}
			if json.Unmarshal(line, &e) == nil {
				if ts, ok := parseTS(e.Timestamp); ok {
					s.data.SessionStart = ts
				}
			}
		}
		return
	}
	var e tEntry
	if json.Unmarshal(line, &e) != nil {
		s.lastUsageKey = ""
		return
	}
	ts, tsOK := parseTS(e.Timestamp)

	var usage *tUsage
	if e.Type == "assistant" && len(e.Message) > 0 && e.Message[0] == '{' {
		var m struct {
			Usage *tUsage `json:"usage"`
		}
		if json.Unmarshal(e.Message, &m) == nil {
			usage = m.Usage
		}
	}
	if usage != nil {
		key := usageKey(usage)
		if key != s.lastUsageKey {
			s.data.Tokens.Input += tokenCount(usage.InputTokens)
			s.data.Tokens.Output += tokenCount(usage.OutputTokens)
			s.data.Tokens.CacheCreation += tokenCount(usage.CacheCreationInputTokens)
			s.data.Tokens.CacheRead += tokenCount(usage.CacheReadInputTokens)
		}
		s.lastUsageKey = key
	} else {
		s.lastUsageKey = ""
	}

	if e.Type == "system" && e.Subtype == "compact_boundary" && tsOK {
		if s.data.LastCompactAt.IsZero() || ts.After(s.data.LastCompactAt) {
			s.data.LastCompactAt = ts
			s.data.LastCompactPost = -1
			if len(e.CompactMetadata) > 0 {
				var cm struct {
					PostTokens *float64 `json:"postTokens"`
				}
				if json.Unmarshal(e.CompactMetadata, &cm) == nil && cm.PostTokens != nil &&
					*cm.PostTokens >= 0 && !math.IsInf(*cm.PostTokens, 0) && !math.IsNaN(*cm.PostTokens) {
					s.data.LastCompactPost = int64(*cm.PostTokens)
				}
			}
		}
	}

	if tsOK {
		if s.data.SessionStart.IsZero() {
			s.data.SessionStart = ts
		}
		if e.Type == "assistant" {
			s.data.LastAssistantAt = ts
		}
	}
}

func parseTS(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func tokenCount(p *float64) int64 {
	if p == nil || math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0 {
		return 0
	}
	return int64(*p)
}

func usageKey(u *tUsage) string {
	f := func(p *float64) string {
		if p == nil {
			return "u"
		}
		return strconv.FormatFloat(*p, 'g', -1, 64)
	}
	return f(u.InputTokens) + "|" + f(u.OutputTokens) + "|" + f(u.CacheCreationInputTokens) + "|" + f(u.CacheReadInputTokens)
}

// applyContextFallback 和 claude-hud 的 applyContextWindowFallback 一致，只是快照放在内存里。
func (s *transcriptState) applyContextFallback(cw *ContextWindow, now time.Time) {
	if cw == nil {
		return
	}
	if isSuspiciousZero(cw) {
		snap := s.snap
		b := s.data.LastCompactAt
		postCompact := !b.IsZero() && (snap == nil || b.After(snap.savedAt))
		if postCompact {
			size := num(cw.ContextWindowSize)
			if s.data.LastCompactPost > 0 && size > 0 {
				pct := math.Min(100, math.Max(0, jsRound(float64(s.data.LastCompactPost)/size*100)))
				rem := 100 - pct
				cw.UsedPercentage = &pct
				cw.RemainingPercentage = &rem
			}
		} else if snap != nil {
			used := snap.used
			cw.UsedPercentage = &used
			cw.RemainingPercentage = snap.remaining
			cw.CurrentUsage = snap.usage
			if cw.ContextWindowSize == nil {
				cw.ContextWindowSize = snap.size
			}
		}
	}
	if num(cw.ContextWindowSize) > 0 && cw.UsedPercentage != nil && *cw.UsedPercentage > 0 {
		s.snap = &ctxSnapshot{used: *cw.UsedPercentage, remaining: cw.RemainingPercentage,
			usage: cw.CurrentUsage, size: cw.ContextWindowSize, savedAt: now}
	}
}

func isSuspiciousZero(cw *ContextWindow) bool {
	if num(cw.UsedPercentage) != 0 {
		return false
	}
	u := cw.CurrentUsage
	return u == nil || (num(u.InputTokens) == 0 && num(u.OutputTokens) == 0 &&
		num(u.CacheCreationInputTokens) == 0 && num(u.CacheReadInputTokens) == 0)
}
