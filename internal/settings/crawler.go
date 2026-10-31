package settings

import "encoding/json"

// CrawlerSettings 爬虫分组配置，字段使用指针以区分「未设置」与零值。
type CrawlerSettings struct {
	Enabled *bool `json:"enabled"`
	Port    *int  `json:"port"`
}

// ParseCrawlerSettings 解析 app_settings 中 crawler 行的 JSON，格式错误时返回 false。
func ParseCrawlerSettings(raw string) (CrawlerSettings, bool) {
	var parsed CrawlerSettings
	if raw == "" {
		return parsed, false
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return CrawlerSettings{}, false
	}
	return parsed, true
}
