package main

import (
	"fmt"
	"math"
	"time"
)

const (
	fiveHourWindow = 5 * time.Hour
	sevenDayWindow = 7 * 24 * time.Hour
)

// usageLine 显示订阅额度（5 小时窗口和本周），逻辑和 claude-hud 的 renderUsageLine 一致。
// 用 API key 或第三方中转时 Claude Code 不提供额度数据，这一项不显示。
func usageLine(f *frame, align bool) string {
	d := f.cfg.Display
	if !d.ShowUsage {
		return ""
	}
	u := usageFromStdin(f.in)
	if u == nil {
		return ""
	}
	if providerLabel(f.in, f.env) == "Bedrock" || isBedrockID(f.in.modelID()) {
		return ""
	}
	p := f.p
	label := progressLabel(f, "label.usage", align)
	tf := d.TimeFormat
	limitTF := limitTimeFormat(tf)
	resetsKey := "format.resetsIn"
	if limitTF == "absolute" {
		resetsKey = "format.resets"
	}

	if u.FiveHour == 100 || u.SevenDay == 100 {
		at := u.SevenDayResetAt
		if u.FiveHour == 100 {
			at = u.FiveHourResetAt
		}
		rt := formatResetTime(at, limitTF, f.now, f.t)
		if d.UsageCompact {
			s := "⚠ Limit"
			if rt != "" {
				s += " (" + rt + ")"
			}
			return p.critical(s)
		}
		suffix := ""
		if rt != "" {
			if d.ShowResetLabel {
				suffix = " (" + f.t.T(resetsKey) + " " + rt + ")"
			} else {
				suffix = " (" + rt + ")"
			}
		}
		return label + " " + p.critical("⚠ "+f.t.T("status.limitReached")+suffix)
	}

	if max(u.FiveHour, u.SevenDay, 0) < d.UsageThreshold {
		return ""
	}

	if d.UsageCompact {
		var five, seven string
		if u.FiveHour >= 0 {
			five = compactWindowPart(f, "5h", u.FiveHour, u.FiveHourResetAt, fiveHourWindow)
		}
		if u.SevenDay >= 0 && (u.FiveHour < 0 || u.SevenDay >= d.SevenDayThreshold) {
			seven = compactWindowPart(f, "7d", u.SevenDay, u.SevenDayResetAt, sevenDayWindow)
		}
		switch {
		case five != "" && seven != "":
			return five + " | " + seven
		case five != "":
			return five
		default:
			return seven
		}
	}

	barWidth := adaptiveBarWidth(f.width)
	if u.FiveHour < 0 && u.SevenDay >= 0 {
		return label + " " + windowPart(f, "", "label.weekly", u.SevenDay, u.SevenDayResetAt, sevenDayWindow, barWidth, true, align)
	}
	five := windowPart(f, "5h", "", u.FiveHour, u.FiveHourResetAt, fiveHourWindow, barWidth, false, align)
	if u.SevenDay >= 0 && u.SevenDay >= d.SevenDayThreshold {
		seven := windowPart(f, "", "label.weekly", u.SevenDay, u.SevenDayResetAt, sevenDayWindow, barWidth, true, align)
		return label + " " + five + " | " + seven
	}
	return label + " " + five
}

func usagePercent(f *frame, pct int) string {
	if pct < 0 {
		return f.p.label("--")
	}
	shown := pct
	if f.cfg.Display.UsageValue == "remaining" {
		shown = max(0, 100-pct)
	}
	return f.p.quotaColor(pct) + fmt.Sprintf("%d%%", shown) + ansiReset
}

func compactWindowPart(f *frame, name string, pct int, resetAt time.Time, window time.Duration) string {
	reset := windowTime(resetAt, window, f.cfg.Display.TimeFormat, f.now, f.t)
	s := f.p.label(name+":") + " " + usagePercent(f, pct)
	if reset != "" {
		s += " " + f.p.label("("+reset+")")
	}
	return s
}

func windowPart(f *frame, name, labelKey string, pct int, resetAt time.Time, window time.Duration, barWidth int, forceLabel, align bool) string {
	d := f.cfg.Display
	tf := d.TimeFormat
	reset := windowTime(resetAt, window, tf, f.now, f.t)
	var styled string
	if labelKey != "" {
		styled = progressLabel(f, labelKey, align)
	} else {
		styled = f.p.label(name)
	}
	resetsKey := "format.resetsIn"
	if tf == "absolute" {
		resetsKey = "format.resets"
	}
	suffix := ""
	if reset != "" {
		if d.ShowResetLabel && tf != "elapsed" && tf != "elapsedAndAbsolute" {
			suffix = "(" + f.t.T(resetsKey) + " " + reset + ")"
		} else {
			suffix = "(" + reset + ")"
		}
	}
	value := usagePercent(f, pct)
	if d.UsageBarEnabled {
		body := f.p.bar(max(pct, 0), barWidth, f.p.quotaColor(max(pct, 0))) + " " + value
		if suffix != "" {
			body += " " + suffix
		}
		if forceLabel {
			return styled + " " + body
		}
		return body
	}
	if suffix != "" {
		return styled + " " + value + " " + suffix
	}
	return styled + " " + value
}

func limitTimeFormat(tf string) string {
	switch tf {
	case "elapsedAndAbsolute":
		return "absolute"
	case "elapsed":
		return "relative"
	case "absolute", "both":
		return tf
	}
	return "relative"
}

func windowTime(resetAt time.Time, window time.Duration, tf string, now time.Time, t translator) string {
	switch tf {
	case "elapsed":
		return elapsedWindow(resetAt, window, now)
	case "elapsedAndAbsolute":
		e := elapsedWindow(resetAt, window, now)
		a := formatResetTime(resetAt, "absolute", now, t)
		if e != "" && a != "" {
			return e + ", " + a
		}
		if e != "" {
			return e
		}
		return a
	case "absolute", "both":
		return formatResetTime(resetAt, tf, now, t)
	}
	return formatResetTime(resetAt, "relative", now, t)
}

func elapsedWindow(resetAt time.Time, window time.Duration, now time.Time) string {
	if resetAt.IsZero() {
		return ""
	}
	start := resetAt.Add(-window)
	pct := int(math.Max(0, math.Min(100, jsRound(float64(now.Sub(start))/float64(window)*100))))
	return fmt.Sprintf("%d%% elapsed", pct)
}

func formatResetTime(resetAt time.Time, mode string, now time.Time, t translator) string {
	if resetAt.IsZero() {
		return ""
	}
	diff := resetAt.Sub(now)
	if diff <= 0 {
		return ""
	}
	if mode == "relative" {
		return formatRelative(diff)
	}
	abs := formatAbsolute(resetAt, now, t)
	if mode == "absolute" {
		return abs
	}
	return formatRelative(diff) + ", " + abs
}

func formatRelative(d time.Duration) string {
	mins := int(math.Ceil(float64(d.Milliseconds()) / 60000))
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	h, m := mins/60, mins%60
	if h >= 24 {
		days, rem := h/24, h%24
		if rem > 0 {
			return fmt.Sprintf("%dd %dh", days, rem)
		}
		return fmt.Sprintf("%dd", days)
	}
	if m > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dh", h)
}

// formatAbsolute 用 24 小时制，跨天时带日期。claude-hud 用的是系统区域格式，这里固定写法。
func formatAbsolute(at, now time.Time, t translator) string {
	at = at.Local()
	now = now.Local()
	prefix := ""
	if a := t.T("format.at"); a != "" {
		prefix = a + " "
	}
	clock := at.Format("15:04")
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return prefix + clock
	}
	var date string
	if t.cjk() {
		date = fmt.Sprintf("%d月%d日", int(at.Month()), at.Day())
	} else {
		date = at.Format("Jan 2")
	}
	return prefix + date + " " + clock
}
