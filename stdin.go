package main

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// StdinData 是 Claude Code 通过 stdin 传给状态栏命令的 JSON。
type StdinData struct {
	Model *struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Cwd       string `json:"cwd"`
	Workspace *struct {
		CurrentDir string `json:"current_dir"`
		AddedDirs  []any  `json:"added_dirs"`
	} `json:"workspace"`
	TranscriptPath string `json:"transcript_path"`
	Version        string `json:"version"`
	Cost           *struct {
		TotalCostUSD *float64 `json:"total_cost_usd"`
	} `json:"cost"`
	ContextWindow *ContextWindow `json:"context_window"`
	RateLimits    *struct {
		FiveHour *RateWindow `json:"five_hour"`
		SevenDay *RateWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type ContextWindow struct {
	ContextWindowSize   *float64 `json:"context_window_size"`
	UsedPercentage      *float64 `json:"used_percentage"`
	RemainingPercentage *float64 `json:"remaining_percentage"`
	CurrentUsage        *Usage   `json:"current_usage"`
}

type Usage struct {
	InputTokens              *float64 `json:"input_tokens"`
	OutputTokens             *float64 `json:"output_tokens"`
	CacheCreationInputTokens *float64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *float64 `json:"cache_read_input_tokens"`
}

type RateWindow struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       *float64 `json:"resets_at"`
}

func num(p *float64) float64 {
	if p == nil || math.IsNaN(*p) || math.IsInf(*p, 0) {
		return 0
	}
	return *p
}

func (in *StdinData) modelID() string {
	if in.Model == nil {
		return ""
	}
	return strings.TrimSpace(in.Model.ID)
}

func (in *StdinData) totalTokens() float64 {
	if in.ContextWindow == nil || in.ContextWindow.CurrentUsage == nil {
		return 0
	}
	u := in.ContextWindow.CurrentUsage
	return num(u.InputTokens) + num(u.CacheCreationInputTokens) + num(u.CacheReadInputTokens)
}

func (in *StdinData) windowSize() float64 {
	if in.ContextWindow == nil {
		return 0
	}
	return num(in.ContextWindow.ContextWindowSize)
}

// jsRound 和 JavaScript 的 Math.round 一致：.5 向正无穷取整。
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

func clampPct(x float64) int { return int(math.Min(100, math.Max(0, x))) }

func (in *StdinData) nativePercent() (int, bool) {
	if in.ContextWindow == nil || in.ContextWindow.UsedPercentage == nil {
		return 0, false
	}
	v := *in.ContextWindow.UsedPercentage
	if math.IsNaN(v) || v <= 0 {
		return 0, false
	}
	return clampPct(jsRound(v)), true
}

const autocompactBufferPercent = 0.165

// contextPercent 对应 claude-hud 的 getContextPercent / getBufferedPercent。
func (in *StdinData) contextPercent(autoCompactWindow int, buffered bool) int {
	total := in.totalTokens()
	if autoCompactWindow > 0 {
		return int(math.Min(100, jsRound(total/float64(autoCompactWindow)*100)))
	}
	if p, ok := in.nativePercent(); ok {
		return p
	}
	size := in.windowSize()
	if size <= 0 {
		return 0
	}
	if !buffered {
		return int(math.Min(100, jsRound(total/size*100)))
	}
	ratio := total / size
	scale := math.Min(1, math.Max(0, (ratio-0.05)/(0.50-0.05)))
	buffer := size * autocompactBufferPercent * scale
	return int(math.Min(100, jsRound((total+buffer)/size*100)))
}

var enterpriseAliases = map[string]string{
	"opusplan": "Claude Opus", "sonnetplan": "Claude Sonnet", "haikuplan": "Claude Haiku",
}

func (in *StdinData) modelName() string {
	if in.Model != nil {
		if d := strings.TrimSpace(in.Model.DisplayName); d != "" {
			return d
		}
	}
	id := in.modelID()
	if id == "" {
		return "Unknown"
	}
	if l, ok := enterpriseAliases[strings.ToLower(id)]; ok {
		return l
	}
	if l := bedrockLabel(id); l != "" {
		return l
	}
	return id
}

func isBedrockID(id string) bool { return strings.Contains(strings.ToLower(id), "anthropic.claude-") }
func isVertexID(id string) bool  { return strings.Contains(id, "@") }

var (
	bedrockSuffixVer  = regexp.MustCompile(`-v\d+:\d+$`)
	bedrockSuffixDate = regexp.MustCompile(`-\d{8}$`)
	digitsOnly        = regexp.MustCompile(`^\d+$`)
	contextSuffix     = regexp.MustCompile(`(?i)\s*\([^)]*\bcontext\b[^)]*\)`)
	claudePrefix      = regexp.MustCompile(`(?i)^Claude\s+`)
)

func bedrockLabel(id string) string {
	lower := strings.ToLower(id)
	i := strings.Index(lower, "anthropic.claude-")
	if i < 0 {
		return ""
	}
	s := lower[i+len("anthropic.claude-"):]
	s = bedrockSuffixVer.ReplaceAllString(s, "")
	s = bedrockSuffixDate.ReplaceAllString(s, "")
	var tokens []string
	for _, t := range strings.Split(s, "-") {
		if t != "" {
			tokens = append(tokens, t)
		}
	}
	fam := -1
	for i, t := range tokens {
		if t == "haiku" || t == "sonnet" || t == "opus" {
			fam = i
			break
		}
	}
	if fam < 0 {
		return ""
	}
	readVer := func(start, step int) []string {
		var parts []string
		for i := start; i >= 0 && i < len(tokens) && len(parts) < 2; i += step {
			if !digitsOnly.MatchString(tokens[i]) {
				break
			}
			parts = append(parts, tokens[i])
		}
		return parts
	}
	before := readVer(fam-1, -1)
	for l, r := 0, len(before)-1; l < r; l, r = l+1, r-1 {
		before[l], before[r] = before[r], before[l]
	}
	after := readVer(fam+1, 1)
	ver := after
	if len(before) >= len(after) {
		ver = before
	}
	label := "Claude " + strings.ToUpper(tokens[fam][:1]) + tokens[fam][1:]
	if len(ver) > 0 {
		label += " " + strings.Join(ver, ".")
	}
	return label
}

func formatModelName(name, format, override string) string {
	if override != "" {
		return override
	}
	if format == "" || format == "full" {
		return name
	}
	r := strings.TrimSpace(contextSuffix.ReplaceAllString(name, ""))
	if format == "short" {
		r = claudePrefix.ReplaceAllString(r, "")
	}
	return r
}

func providerLabel(in *StdinData, env map[string]string) string {
	switch {
	case env["CLAUDE_CODE_USE_BEDROCK"] == "1":
		return "Bedrock"
	case env["CLAUDE_CODE_USE_VERTEX"] == "1":
		return "Vertex"
	}
	if _, ok := enterpriseAliases[strings.ToLower(in.modelID())]; ok {
		return "Enterprise"
	}
	return ""
}

// UsageData 是订阅额度（5 小时窗口和 7 天窗口）。百分比为 -1 表示没有数据。
type UsageData struct {
	FiveHour, SevenDay               int
	FiveHourResetAt, SevenDayResetAt time.Time
}

func usageFromStdin(in *StdinData) *UsageData {
	rl := in.RateLimits
	if rl == nil {
		return nil
	}
	pct := func(w *RateWindow) int {
		if w == nil || w.UsedPercentage == nil || math.IsNaN(*w.UsedPercentage) || math.IsInf(*w.UsedPercentage, 0) {
			return -1
		}
		return int(jsRound(math.Min(100, math.Max(0, *w.UsedPercentage))))
	}
	reset := func(w *RateWindow) time.Time {
		if w == nil || w.ResetsAt == nil || *w.ResetsAt <= 0 || math.IsInf(*w.ResetsAt, 0) {
			return time.Time{}
		}
		return time.UnixMilli(int64(*w.ResetsAt * 1000))
	}
	u := &UsageData{FiveHour: pct(rl.FiveHour), SevenDay: pct(rl.SevenDay),
		FiveHourResetAt: reset(rl.FiveHour), SevenDayResetAt: reset(rl.SevenDay)}
	if u.FiveHour < 0 && u.SevenDay < 0 {
		return nil
	}
	return u
}

// 费用：优先用 Claude Code 给的 total_cost_usd；没有时按令牌数估算，价格表和 claude-hud 一致。
type pricing struct{ in, out float64 }

var modelPricing = []struct {
	re *regexp.Regexp
	p  pricing
}{
	{regexp.MustCompile(`(?i)\bopus 4(?: \d+)?\b`), pricing{15, 75}},
	{regexp.MustCompile(`(?i)\bsonnet 4(?: \d+)?\b`), pricing{3, 15}},
	{regexp.MustCompile(`(?i)\bsonnet 3 7\b`), pricing{3, 15}},
	{regexp.MustCompile(`(?i)\bsonnet 3 5\b`), pricing{3, 15}},
	{regexp.MustCompile(`(?i)\bhaiku 4(?: \d+)?\b`), pricing{1, 5}},
	{regexp.MustCompile(`(?i)\bhaiku 3 5\b`), pricing{0.8, 4}},
	{regexp.MustCompile(`(?i)\bopusplan\b`), pricing{15, 75}},
	{regexp.MustCompile(`(?i)\bsonnetplan\b`), pricing{3, 15}},
	{regexp.MustCompile(`(?i)\bhaikuplan\b`), pricing{0.8, 4}},
}

var (
	reClaudeLead = regexp.MustCompile(`^claude\s+`)
	reParens     = regexp.MustCompile(`\([^)]*\)`)
	reSeps       = regexp.MustCompile(`[._-]+`)
	reSpaces     = regexp.MustCompile(`\s+`)
)

func matchPricing(name string) (pricing, bool) {
	n := strings.ToLower(name)
	n = reClaudeLead.ReplaceAllString(n, "")
	n = reParens.ReplaceAllString(n, " ")
	n = reSeps.ReplaceAllString(n, " ")
	n = strings.TrimSpace(reSpaces.ReplaceAllString(n, " "))
	for _, e := range modelPricing {
		if e.re.MatchString(n) {
			return e.p, true
		}
	}
	return pricing{}, false
}

// sessionCost 返回费用和是否来自 Claude Code 自身（false 表示估算）。
func sessionCost(in *StdinData, tok SessionTokens, hasTok bool) (float64, bool, bool) {
	id := in.modelID()
	if isBedrockID(id) || isVertexID(id) {
		return 0, false, false
	}
	if in.Cost != nil && in.Cost.TotalCostUSD != nil && !math.IsNaN(*in.Cost.TotalCostUSD) && !math.IsInf(*in.Cost.TotalCostUSD, 0) {
		return *in.Cost.TotalCostUSD, true, true
	}
	if !hasTok || tok.total() == 0 {
		return 0, false, false
	}
	var p pricing
	found := false
	if in.Model != nil {
		for _, c := range []string{strings.TrimSpace(in.Model.DisplayName), id} {
			if c == "" {
				continue
			}
			if p, found = matchPricing(c); found {
				break
			}
		}
	}
	if !found {
		return 0, false, false
	}
	usd := (float64(tok.Input)*p.in + float64(tok.CacheCreation)*p.in*1.25 +
		float64(tok.CacheRead)*p.in*0.1 + float64(tok.Output)*p.out) / 1e6
	return usd, false, true
}

func formatUSD(v float64) string {
	switch {
	case v >= 1:
		return fmt.Sprintf("$%.2f", v)
	case v >= 0.1:
		return fmt.Sprintf("$%.3f", v)
	default:
		return fmt.Sprintf("$%.4f", v)
	}
}

// formatTokens 和 claude-hud 一致：1.2M、45k、800。
func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", math.Floor(float64(n)/1e6*10+0.5)/10)
	case n >= 1000:
		return fmt.Sprintf("%.0fk", jsFixed0(float64(n)/1000))
	default:
		return fmt.Sprintf("%d", n)
	}
}

// jsFixed0 让 %.0f 的进位方式和 JavaScript toFixed(0) 一致（.5 远离零）。
func jsFixed0(x float64) float64 { return math.Floor(x + 0.5) }
