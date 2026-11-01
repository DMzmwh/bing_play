//go:build windows

package i18n

import "golang.org/x/sys/windows/registry"

// detectSystemLang 读取 Windows 当前用户界面的区域设置，例如 zh-CN。
func detectSystemLang() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\International`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	name, _, err := key.GetStringValue("LocaleName")
	if err != nil {
		return ""
	}
	return matchSupportedLang(name)
}
