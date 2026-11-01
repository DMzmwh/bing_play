package i18n

import "testing"

func TestMatchSupportedLang(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "简体中文", raw: "zh-CN", want: "zh-CN"},
		{name: "中文无地区", raw: "zh", want: "zh-CN"},
		{name: "下划线写法", raw: "zh_CN.UTF-8", want: "zh-CN"},
		{name: "繁体台湾", raw: "zh-TW", want: "zh-TW"},
		{name: "繁体香港", raw: "zh-HK", want: "zh-TW"},
		{name: "繁体澳门", raw: "zh-MO", want: "zh-TW"},
		{name: "英语", raw: "en-US", want: "en"},
		{name: "英语小写", raw: "en_US.UTF-8", want: "en"},
		{name: "无法识别", raw: "C.UTF-8", want: ""},
		{name: "空值", raw: "  ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchSupportedLang(tt.raw); got != tt.want {
				t.Errorf("matchSupportedLang(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
