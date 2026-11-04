package main

// 界面文字，和 claude-hud 0.1.1 的 en / zh 语言包一致。
var messages = map[string]map[string]string{
	"en": {
		"label.context": "Context", "label.usage": "Usage", "label.weekly": "Weekly",
		"label.promptCache": "Cache", "label.estimatedCost": "Est.", "label.cost": "Cost",
		"label.tokens": "Tokens", "status.limitReached": "Limit reached", "status.expired": "expired",
		"format.resets": "resets", "format.resetsIn": "resets in", "format.at": "at",
		"format.in": "in", "format.cache": "cache", "format.out": "out",
	},
	"zh": {
		"label.context": "上下文", "label.usage": "用量", "label.weekly": "本周",
		"label.promptCache": "缓存", "label.estimatedCost": "估算", "label.cost": "费用",
		"label.tokens": "令牌", "status.limitReached": "已达上限", "status.expired": "已过期",
		"format.resets": "重置于", "format.resetsIn": "重置剩余", "format.at": "",
		"format.in": "输入", "format.cache": "缓存", "format.out": "输出",
	},
}

type translator struct {
	lang string // "en" 或 "zh"
}

func newTranslator(language string) translator {
	if language == "zh" || language == "zh-Hans" {
		return translator{"zh"}
	}
	return translator{"en"}
}

func (t translator) cjk() bool { return t.lang == "zh" }

func (t translator) T(key string) string {
	if v, ok := messages[t.lang][key]; ok {
		return v
	}
	if v, ok := messages["en"][key]; ok {
		return v
	}
	return key
}
