//go:build !windows

package i18n

import "os"

// detectSystemLang 读取 POSIX 系统的 LANG / LC_ALL 环境变量，例如 zh_CN.UTF-8。
func detectSystemLang() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(key); value != "" {
			if lang := matchSupportedLang(value); lang != "" {
				return lang
			}
		}
	}
	return ""
}
