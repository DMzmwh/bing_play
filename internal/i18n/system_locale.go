package i18n

import "strings"

// matchSupportedLang 把系统区域设置规范化为项目支持的语言代码。
// 支持 zh-CN / zh-TW / en，无法识别时返回空字符串。
func matchSupportedLang(raw string) string {
	raw = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "_", "-"))
	switch {
	case raw == "":
		return ""
	case strings.HasPrefix(raw, "zh-tw"), strings.HasPrefix(raw, "zh-hk"), strings.HasPrefix(raw, "zh-mo"):
		return "zh-TW"
	case strings.HasPrefix(raw, "zh"):
		return "zh-CN"
	case strings.HasPrefix(raw, "en"):
		return "en"
	}
	return ""
}
