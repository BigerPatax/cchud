package main

import (
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// frame 是渲染一次需要的全部数据。
type frame struct {
	in      *StdinData
	tr      TranscriptData
	cfg     *Config
	git     *GitStatus
	version string
	width   int // 0 表示不知道
	now     time.Time
	env     map[string]string
	t       translator
	p       palette
}

// render 输出展开布局，每行以重置颜色开头，和 claude-hud 的 render 一致。
func render(f *frame) string {
	lines := renderExpanded(f)
	if f.cfg.Display.ShowSessionTokens {
		if l := sessionTokensLine(f); l != "" {
			lines = append(lines, l)
		}
	}
	var b strings.Builder
	for _, l := range lines {
		for _, phys := range strings.Split(l, "\n") {
			for _, w := range wrapLine(phys, f.width, f.t.cjk()) {
				b.WriteString(ansiReset)
				b.WriteString(w)
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func renderExpanded(f *frame) []string {
	order := f.cfg.ElementOrder
	groupOf := map[string]map[string]bool{}
	for _, g := range f.cfg.Display.MergeGroups {
		set := map[string]bool{}
		for _, e := range g {
			set[e] = true
		}
		for _, e := range g {
			if _, ok := groupOf[e]; !ok {
				groupOf[e] = set
			}
		}
	}
	seen := map[string]bool{}
	var lines []string
	for i := 0; i < len(order); i++ {
		el := order[i]
		if seen[el] {
			continue
		}
		if g := groupOf[el]; g != nil {
			var seq []string
			for j := i; j < len(order) && !seen[order[j]] && g[order[j]]; j++ {
				seq = append(seq, order[j])
			}
			if len(seq) > 1 {
				i += len(seq) - 1
				type rendered struct{ el, line string }
				var rs []rendered
				for _, e := range seq {
					seen[e] = true
					if l := elementLine(f, e, false); l != "" {
						rs = append(rs, rendered{e, l})
					}
				}
				switch {
				case len(rs) > 1:
					parts := make([]string, len(rs))
					for k, r := range rs {
						parts[k] = r.line
					}
					combined := strings.Join(parts, " │ ")
					if f.width == 0 || visibleWidth(combined, f.t.cjk()) <= f.width {
						lines = append(lines, combined)
					} else {
						for _, r := range rs {
							if l := elementLine(f, r.el, true); l != "" {
								lines = append(lines, l)
							} else {
								lines = append(lines, r.line)
							}
						}
					}
				case len(rs) == 1:
					lines = append(lines, rs[0].line)
				}
				continue
			}
		}
		seen[el] = true
		if l := elementLine(f, el, false); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// elementLine 返回一个元素的整行文字，不显示时返回空串。
// memory、environment、tools、skills、mcp、agents、todos、sessionTime 暂未移植。
func elementLine(f *frame, el string, align bool) string {
	switch el {
	case "project":
		return projectLine(f)
	case "addedDirs":
		return addedDirsLine(f)
	case "context":
		return contextLine(f, align)
	case "usage":
		return usageLine(f, align)
	case "promptCache":
		return promptCacheLine(f)
	}
	return ""
}

var unsafeDisplay = regexp.MustCompile("[\u0000-\u001F\u007F-\u009F؜‎‏‪-‮⁦-⁩⁪-⁯]")

func sanitize(s string) string { return unsafeDisplay.ReplaceAllString(s, "") }

func fileHref(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	abs = filepath.ToSlash(abs)
	if !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

func splitPath(p string) []string {
	var segs []string
	for _, s := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

func projectLine(f *frame) string {
	d := f.cfg.Display
	p := f.p
	var parts []string
	if d.CustomLine != "" && d.CustomLinePosition == "first" {
		parts = append(parts, p.custom(d.CustomLine))
	}
	if d.ShowModel {
		name := formatModelName(f.in.modelName(), d.ModelFormat, d.ModelOverride)
		if pl := providerLabel(f.in, f.env); pl != "" {
			name += " | " + pl
		}
		parts = append(parts, p.model("["+name+"]"))
	}

	projectPart := ""
	if d.ShowProject && f.in.Cwd != "" {
		segs := splitPath(f.in.Cwd)
		shown := "/"
		if len(segs) > 0 {
			n := f.cfg.PathLevels
			if n > len(segs) {
				n = len(segs)
			}
			shown = strings.Join(segs[len(segs)-n:], "/")
		}
		projectPart = hyperlink(fileHref(f.in.Cwd), p.project(sanitize(shown)))
	}
	dirsPart := ""
	if d.ShowAddedDirs && d.AddedDirsLayout == "inline" {
		dirsPart = addedDirsText(f, "+")
	}

	gitPart := ""
	g := f.cfg.Git
	if g.Enabled && f.git != nil {
		branch := f.git.Branch
		if g.ShowDirty && f.git.Dirty {
			branch += "*"
		}
		inner := []string{hyperlink(f.git.BranchURL, p.gitBranch(sanitize(branch)))}
		if g.ShowAheadBehind {
			if f.git.Ahead > 0 {
				v := fmt.Sprintf("↑%d", f.git.Ahead)
				switch {
				case g.PushCriticalThreshold > 0 && f.git.Ahead >= g.PushCriticalThreshold:
					v = p.critical(v)
				case g.PushWarningThreshold > 0 && f.git.Ahead >= g.PushWarningThreshold:
					v = p.warning(v)
				default:
					v = p.gitBranch(v)
				}
				inner = append(inner, v)
			}
			if f.git.Behind > 0 {
				inner = append(inner, p.gitBranch(fmt.Sprintf("↓%d", f.git.Behind)))
			}
		}
		gitPart = p.git("git:(") + strings.Join(inner, " ") + p.git(")")
	}

	proj := projectPart
	if proj != "" && dirsPart != "" {
		proj += " " + dirsPart
	} else if proj == "" {
		proj = dirsPart
	}
	switch {
	case proj != "" && gitPart != "":
		if g.BranchOverflow == "wrap" {
			parts = append(parts, proj, gitPart)
		} else {
			parts = append(parts, proj+" "+gitPart)
		}
	case proj != "":
		parts = append(parts, proj)
	case gitPart != "":
		parts = append(parts, gitPart)
	}

	if d.ShowClaudeCodeVersion && f.version != "" {
		parts = append(parts, p.label("CC v"+f.version))
	}
	if d.ShowDuration {
		if dur := sessionDuration(f.tr.SessionStart, f.now); dur != "" {
			parts = append(parts, p.custom("⏱️  "+dur))
		}
	}
	if d.ShowCost {
		if usd, native, ok := sessionCost(f.in, f.tr.Tokens, f.tr.Valid); ok {
			key := "label.estimatedCost"
			if native {
				key = "label.cost"
			}
			parts = append(parts, paint(f.t.T(key)+" 💵 "+formatUSD(usd), ansiYellow))
		}
	}
	if d.CustomLine != "" && d.CustomLinePosition == "last" {
		parts = append(parts, p.custom(d.CustomLine))
	}
	return strings.Join(parts, " │ ")
}

const maxAddedDirs = 5
const addedDirNameMax = 24

// addedDirsText 显示 /add-dir 加入的目录名，最多 5 个。
func addedDirsText(f *frame, prefix string) string {
	if f.in.Workspace == nil {
		return ""
	}
	var dirs []string
	for _, v := range f.in.Workspace.AddedDirs {
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		if segs := splitPath(s); len(segs) == 0 || sanitize(segs[len(segs)-1]) == "" {
			continue
		}
		dirs = append(dirs, s)
	}
	if len(dirs) == 0 {
		return ""
	}
	shown := dirs
	if len(shown) > maxAddedDirs {
		shown = shown[:maxAddedDirs]
	}
	var out []string
	for _, dir := range shown {
		segs := splitPath(dir)
		name := sanitize(segs[len(segs)-1])
		if r := []rune(name); len(r) > addedDirNameMax {
			name = string(r[:addedDirNameMax-1]) + "…"
		}
		out = append(out, hyperlink(fileHref(dir), paint(prefix+name, ansiDim)))
	}
	if extra := len(dirs) - len(shown); extra > 0 {
		out = append(out, paint(fmt.Sprintf("+%d more", extra), ansiDim))
	}
	return strings.Join(out, " ")
}

func addedDirsLine(f *frame) string {
	d := f.cfg.Display
	if !d.ShowAddedDirs || d.AddedDirsLayout != "line" {
		return ""
	}
	dirs := addedDirsText(f, "")
	if dirs == "" {
		return ""
	}
	return f.p.label("Added dirs:") + " " + dirs
}

func sessionDuration(start, now time.Time) string {
	if start.IsZero() {
		return ""
	}
	mins := int(math.Floor(float64(now.Sub(start).Milliseconds()) / 60000))
	switch {
	case mins < 1:
		return "<1m"
	case mins < 60:
		return fmt.Sprintf("%dm", mins)
	default:
		return fmt.Sprintf("%dh %dm", mins/60, mins%60)
	}
}

// progressLabel 在多行堆叠时把"上下文"、"用量"、"本周"补齐到同样宽度。
func progressLabel(f *frame, key string, align bool) string {
	text := f.t.T(key)
	if align {
		max := 0
		for _, k := range []string{"label.context", "label.usage", "label.weekly"} {
			if w := textWidth(f.t.T(k), f.t.cjk()); w > max {
				max = w
			}
		}
		if pad := max - textWidth(text, f.t.cjk()); pad > 0 {
			text += strings.Repeat(" ", pad)
		}
	}
	return f.p.label(text)
}

// adaptiveBarWidth 按终端宽度选进度条长度：100 列以上 10 格，60 到 99 列 6 格，更窄 4 格。
func adaptiveBarWidth(width int) int {
	switch {
	case width == 0 || width >= 100:
		return 10
	case width >= 60:
		return 6
	default:
		return 4
	}
}

func contextLine(f *frame, align bool) string {
	d := f.cfg.Display
	pct := f.in.contextPercent(d.AutoCompactWindow, d.AutocompactBuffer != "disabled")
	color := f.p.contextColor(pct, d.ContextWarningThreshold, d.ContextCriticalThreshold)
	value := color + contextValue(f, pct) + ansiReset
	label := progressLabel(f, "label.context", align)
	var line string
	if d.ShowContextBar {
		w := d.ContextBarWidth
		if w <= 0 {
			w = adaptiveBarWidth(f.width)
		}
		line = label + " 🌐  " + f.p.bar(pct, w, color) + " " + value
	} else {
		line = label + " 🌐  " + value
	}
	if d.ShowTokenBreakdown && pct >= d.ContextCriticalThreshold && f.in.ContextWindow != nil && f.in.ContextWindow.CurrentUsage != nil {
		u := f.in.ContextWindow.CurrentUsage
		in := formatTokens(int64(num(u.InputTokens)))
		cache := formatTokens(int64(num(u.CacheCreationInputTokens) + num(u.CacheReadInputTokens)))
		line += f.p.label(fmt.Sprintf(" (%s: %s, %s: %s)", f.t.T("format.in"), in, f.t.T("format.cache"), cache))
	}
	return line
}

func contextValue(f *frame, pct int) string {
	d := f.cfg.Display
	total := int64(f.in.totalTokens())
	size := int64(f.in.windowSize())
	if d.AutoCompactWindow > 0 {
		size = int64(d.AutoCompactWindow)
	}
	switch d.ContextValue {
	case "tokens":
		if size > 0 {
			return formatTokens(total) + "/" + formatTokens(size)
		}
		return formatTokens(total)
	case "both":
		if size > 0 {
			return fmt.Sprintf("%d%% (%s/%s)", pct, formatTokens(total), formatTokens(size))
		}
		return fmt.Sprintf("%d%%", pct)
	case "remaining":
		return fmt.Sprintf("%d%%", max(0, 100-pct))
	}
	return fmt.Sprintf("%d%%", pct)
}

func promptCacheLine(f *frame) string {
	d := f.cfg.Display
	if !d.ShowPromptCache || f.tr.LastAssistantAt.IsZero() {
		return ""
	}
	ttl := d.PromptCacheTTLSeconds
	if ttl <= 0 {
		ttl = 300
	}
	remaining := f.tr.LastAssistantAt.Add(time.Duration(ttl) * time.Second).Sub(f.now)
	warnSec := min(ttl, max(60, ttl/5))
	value := "♻️ " + cacheCountdown(remaining, f.t)
	switch {
	case remaining <= 0:
		value = f.p.label(value)
	case remaining <= time.Duration(warnSec)*time.Second:
		value = f.p.warning(value)
	default:
		value = paint(value, ansiOrange)
	}
	return f.p.label(f.t.T("label.promptCache")) + " " + value
}

func cacheCountdown(remaining time.Duration, t translator) string {
	if remaining <= 0 {
		return t.T("status.expired")
	}
	total := int(math.Ceil(float64(remaining.Milliseconds()) / 1000))
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}

func sessionTokensLine(f *frame) string {
	tok := f.tr.Tokens
	if !f.tr.Valid || tok.total() == 0 {
		return ""
	}
	parts := []string{
		f.t.T("format.in") + ": " + paint(formatTokens(tok.Input), ansiGreen),
		f.t.T("format.out") + ": " + paint(formatTokens(tok.Output), ansiYellow),
	}
	if tok.CacheCreation > 0 || tok.CacheRead > 0 {
		parts = append(parts, f.t.T("format.cache")+": "+paint(formatTokens(tok.CacheCreation+tok.CacheRead), ansiOrange))
	}
	return f.p.label(f.t.T("label.tokens")) + " " + paint(formatTokens(tok.total()), ansiCyan) + " (" + strings.Join(parts, ", ") + ")"
}
