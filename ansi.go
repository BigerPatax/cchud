package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ansiReset         = "\x1b[0m"
	ansiDim           = "\x1b[2m"
	ansiRed           = "\x1b[31m"
	ansiGreen         = "\x1b[32m"
	ansiYellow        = "\x1b[33m"
	ansiMagenta       = "\x1b[35m"
	ansiCyan          = "\x1b[36m"
	ansiBrightBlue    = "\x1b[94m"
	ansiBrightMagenta = "\x1b[95m"
	ansiOrange        = "\x1b[38;5;208m"
)

var namedColors = map[string]string{
	"dim": ansiDim, "red": ansiRed, "green": ansiGreen, "yellow": ansiYellow, "magenta": ansiMagenta,
	"cyan": ansiCyan, "brightBlue": ansiBrightBlue, "brightMagenta": ansiBrightMagenta,
}

// resolveColor 支持颜色名、0 到 255 的色号、#rrggbb 三种写法。
func resolveColor(v any, fallback string) string {
	switch x := v.(type) {
	case float64:
		return fmt.Sprintf("\x1b[38;5;%dm", int(x))
	case string:
		if strings.HasPrefix(x, "#") && len(x) == 7 {
			var r, g, b int
			if _, err := fmt.Sscanf(x[1:], "%02x%02x%02x", &r, &g, &b); err == nil {
				return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
			}
			return fallback
		}
		if c, ok := namedColors[x]; ok {
			return c
		}
	}
	return fallback
}

func paint(text, color string) string { return color + text + ansiReset }

type palette struct{ colors map[string]any }

func (p palette) get(key, fallback string) string { return resolveColor(p.colors[key], fallback) }
func (p palette) label(s string) string           { return paint(s, p.get("label", ansiDim)) }
func (p palette) model(s string) string           { return paint(s, p.get("model", ansiCyan)) }
func (p palette) project(s string) string         { return paint(s, p.get("project", ansiYellow)) }
func (p palette) git(s string) string             { return paint(s, p.get("git", ansiMagenta)) }
func (p palette) gitBranch(s string) string       { return paint(s, p.get("gitBranch", ansiCyan)) }
func (p palette) custom(s string) string          { return paint(s, p.get("custom", ansiOrange)) }
func (p palette) warning(s string) string         { return paint(s, p.get("warning", ansiYellow)) }
func (p palette) critical(s string) string        { return paint(s, p.get("critical", ansiRed)) }

func (p palette) contextColor(pct, warn, crit int) string {
	switch {
	case pct >= crit:
		return p.get("critical", ansiRed)
	case pct >= warn:
		return p.get("warning", ansiYellow)
	default:
		return p.get("context", ansiGreen)
	}
}

func (p palette) quotaColor(pct int) string {
	switch {
	case pct >= 90:
		return p.get("critical", ansiRed)
	case pct >= 75:
		return p.get("usageWarning", ansiBrightMagenta)
	default:
		return p.get("usage", ansiBrightBlue)
	}
}

func (p palette) barChars() (string, string) {
	f, _ := p.colors["barFilled"].(string)
	e, _ := p.colors["barEmpty"].(string)
	if f == "" {
		f = "█"
	}
	if e == "" {
		e = "░"
	}
	return f, e
}

// bar 和 claude-hud 的 coloredBar / quotaBar 一致：已用部分最后一格用 ▓。
func (p palette) bar(pct, width int, color string) string {
	if width < 0 {
		width = 0
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(jsRound(float64(pct) / 100 * float64(width)))
	fc, ec := p.barChars()
	part := ""
	if filled > 0 {
		part = strings.Repeat(fc, filled-1) + "▓"
	}
	return color + part + ansiDim + strings.Repeat(ec, width-filled) + ansiReset
}

func hyperlink(uri, text string) string {
	if uri == "" {
		return text
	}
	return "\x1b]8;;" + uri + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// ansiLen 返回 s[i:] 开头的转义序列长度，不是转义序列返回 0。
// 识别 SGR（ESC [ 数字 m）和 OSC（ESC ] ... BEL 或 ESC \）。
func ansiLen(s string, i int) int {
	if i+1 >= len(s) || s[i] != 0x1b {
		return 0
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			return j + 1 - i
		}
	case ']':
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1 - i
			}
			if s[j] == 0x1b {
				if j+1 < len(s) && s[j+1] == '\\' {
					return j + 2 - i
				}
				return 0
			}
		}
	}
	return 0
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if n := ansiLen(s, i); n > 0 {
			i += n
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// 显示宽度的计算方式和 claude-hud 一致：中文界面下，制表符、方块等宽度不明确的字符按 2 格算。
func isWideRune(r rune) bool {
	return r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
		(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
		(r >= 0xAC00 && r <= 0xD7A3) || (r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE10 && r <= 0xFE19) || (r >= 0xFE30 && r <= 0xFE6F) ||
		(r >= 0xFF00 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x1F300 && r <= 0x1FAFF) || (r >= 0x20000 && r <= 0x3FFFD))
}

func isAmbiguousWide(r rune) bool {
	return (r >= 0x2010 && r <= 0x2027) || (r >= 0x2030 && r <= 0x205E) ||
		(r >= 0x2190 && r <= 0x21FF) || (r >= 0x2200 && r <= 0x22FF) ||
		(r >= 0x2300 && r <= 0x23FF) || (r >= 0x2460 && r <= 0x24FF) ||
		(r >= 0x2500 && r <= 0x259F) || (r >= 0x25A0 && r <= 0x25FF) ||
		(r >= 0x2600 && r <= 0x26FF) || (r >= 0x2700 && r <= 0x27BF)
}

// 表情符号（Unicode Extended_Pictographic 的主要区段）按 2 格算。
var pictographic = [][2]rune{
	{0x00A9, 0x00A9}, {0x00AE, 0x00AE}, {0x203C, 0x203C}, {0x2049, 0x2049}, {0x2122, 0x2122},
	{0x2139, 0x2139}, {0x2194, 0x2199}, {0x21A9, 0x21AA}, {0x231A, 0x231B}, {0x2328, 0x2328},
	{0x2388, 0x2388}, {0x23CF, 0x23CF}, {0x23E9, 0x23F3}, {0x23F8, 0x23FA}, {0x24C2, 0x24C2},
	{0x25AA, 0x25AB}, {0x25B6, 0x25B6}, {0x25C0, 0x25C0}, {0x25FB, 0x25FE}, {0x2600, 0x2605},
	{0x2607, 0x2612}, {0x2614, 0x2685}, {0x2690, 0x2705}, {0x2708, 0x2712}, {0x2714, 0x2714},
	{0x2716, 0x2716}, {0x271D, 0x271D}, {0x2721, 0x2721}, {0x2728, 0x2728}, {0x2733, 0x2734},
	{0x2744, 0x2744}, {0x2747, 0x2747}, {0x274C, 0x274C}, {0x274E, 0x274E}, {0x2753, 0x2755},
	{0x2757, 0x2757}, {0x2763, 0x2767}, {0x2795, 0x2797}, {0x27A1, 0x27A1}, {0x27B0, 0x27B0},
	{0x27BF, 0x27BF}, {0x2934, 0x2935}, {0x2B05, 0x2B07}, {0x2B1B, 0x2B1C}, {0x2B50, 0x2B50},
	{0x2B55, 0x2B55}, {0x3030, 0x3030}, {0x303D, 0x303D}, {0x3297, 0x3297}, {0x3299, 0x3299},
	{0x1F000, 0x1F0FF}, {0x1F10D, 0x1F10F}, {0x1F12F, 0x1F12F}, {0x1F16C, 0x1F171},
	{0x1F17E, 0x1F17F}, {0x1F18E, 0x1F18E}, {0x1F191, 0x1F19A}, {0x1F1AD, 0x1F1E5},
	{0x1F201, 0x1F20F}, {0x1F21A, 0x1F21A}, {0x1F22F, 0x1F22F}, {0x1F232, 0x1F23A},
	{0x1F23C, 0x1F23F}, {0x1F249, 0x1F3FA}, {0x1F400, 0x1F53D}, {0x1F546, 0x1F64F},
	{0x1F680, 0x1F6FF}, {0x1F774, 0x1F77F}, {0x1F7D5, 0x1F7FF}, {0x1F80C, 0x1F80F},
	{0x1F848, 0x1F84F}, {0x1F85A, 0x1F85F}, {0x1F888, 0x1F88F}, {0x1F8AE, 0x1F8FF},
	{0x1F90C, 0x1F93A}, {0x1F93C, 0x1F945}, {0x1F947, 0x1FAFF}, {0x1FC00, 0x1FFFD},
}

func isPictographic(r rune) bool {
	if r < 0xA9 {
		return false
	}
	for _, rg := range pictographic {
		if r < rg[0] {
			return false
		}
		if r <= rg[1] {
			return true
		}
	}
	return false
}

// textWidth 计算不含转义序列的文本占几格。
// 组合字符、零宽连接符和变体选择符不占宽度，零宽连接符后面的字符并入前一个字形。
func textWidth(s string, cjk bool) int {
	w := 0
	joined := false
	for _, r := range s {
		switch {
		case r == 0x200D:
			joined = true
			continue
		case r == 0xFE0F || unicode.Is(unicode.M, r) || unicode.IsControl(r):
			continue
		}
		if joined {
			joined = false
			continue
		}
		switch {
		case isPictographic(r) || isWideRune(r):
			w += 2
		case cjk && isAmbiguousWide(r):
			w += 2
		default:
			w++
		}
	}
	return w
}

func visibleWidth(s string, cjk bool) int { return textWidth(stripANSI(s), cjk) }

// sliceVisible 截取前 max 格可见内容，转义序列原样保留。
func sliceVisible(s string, max int, cjk bool) string {
	if max <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for i := 0; i < len(s); {
		if n := ansiLen(s, i); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		// 把后面紧跟的组合字符、变体选择符、零宽连接序列一起取出，作为一个字形。
		j := i + size
		for j < len(s) {
			r2, s2 := utf8.DecodeRuneInString(s[j:])
			if r2 == 0xFE0F || unicode.Is(unicode.M, r2) {
				j += s2
				continue
			}
			if r2 == 0x200D && j+s2 < len(s) {
				_, s3 := utf8.DecodeRuneInString(s[j+s2:])
				j += s2 + s3
				continue
			}
			break
		}
		cw := textWidth(string(r), cjk)
		if used+cw > max {
			break
		}
		b.WriteString(s[i:j])
		used += cw
		i = j
	}
	return b.String()
}

// closeOpenHyperlink：截断位置落在超链接中间时补上结束标记。
func closeOpenHyperlink(s string) string {
	last := ""
	found := false
	for i := 0; i < len(s); i++ {
		if strings.HasPrefix(s[i:], "\x1b]8;;") {
			if n := ansiLen(s, i); n > 0 {
				body := s[i+5 : i+n]
				body = strings.TrimSuffix(strings.TrimSuffix(body, "\x07"), "\x1b\\")
				last = body
				found = true
				i += n - 1
			}
		}
	}
	if found && last != "" {
		return "\x1b]8;;\x1b\\"
	}
	return ""
}

func truncateToWidth(s string, max int, cjk bool) string {
	if max <= 0 || visibleWidth(s, cjk) <= max {
		return s
	}
	suffix := "..."
	if max < 3 {
		suffix = strings.Repeat(".", max)
	}
	keep := max - len(suffix)
	if keep < 0 {
		keep = 0
	}
	sl := sliceVisible(s, keep, cjk)
	return sl + closeOpenHyperlink(sl) + suffix + ansiReset
}

type wrapPart struct{ sep, seg string }

// splitWrapParts 按 " | " 和 " │ " 切分，开头的 [模型 | 提供方] 不拆开。
func splitWrapParts(line string) []wrapPart {
	var segs, seps []string
	start := 0
	for i := 0; i < len(line); {
		if n := ansiLen(line, i); n > 0 {
			i += n
			continue
		}
		sep := ""
		if strings.HasPrefix(line[i:], " | ") {
			sep = " | "
		} else if strings.HasPrefix(line[i:], " │ ") {
			sep = " │ "
		}
		if sep != "" {
			segs = append(segs, line[start:i])
			seps = append(seps, sep)
			i += len(sep)
			start = i
			continue
		}
		i++
	}
	segs = append(segs, line[start:])
	parts := []wrapPart{{"", segs[0]}}
	for k := 1; k < len(segs); k++ {
		parts = append(parts, wrapPart{seps[k-1], segs[k]})
	}
	first := stripANSI(parts[0].seg)
	if strings.HasPrefix(strings.TrimLeft(first, " \t"), "[") && !strings.Contains(first, "]") && len(parts) > 1 {
		merged := parts[0].seg
		k := 1
		for k < len(parts) {
			merged += parts[k].sep + parts[k].seg
			k++
			if strings.Contains(stripANSI(parts[k-1].seg), "]") {
				break
			}
		}
		parts = append([]wrapPart{{"", merged}}, parts[k:]...)
	}
	return parts
}

// wrapLine 超宽时在分隔符处换行，单段仍然超宽就截断加省略号。
func wrapLine(line string, max int, cjk bool) []string {
	if max <= 0 || visibleWidth(line, cjk) <= max {
		return []string{line}
	}
	parts := splitWrapParts(line)
	if len(parts) <= 1 {
		return []string{truncateToWidth(line, max, cjk)}
	}
	var out []string
	cur := parts[0].seg
	for _, p := range parts[1:] {
		cand := cur + p.sep + p.seg
		if visibleWidth(cand, cjk) <= max {
			cur = cand
			continue
		}
		out = append(out, truncateToWidth(cur, max, cjk))
		cur = p.seg
	}
	if cur != "" {
		out = append(out, truncateToWidth(cur, max, cjk))
	}
	return out
}
