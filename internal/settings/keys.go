// Package settings 定义 app_settings 表中使用的配置键名与分组结构。
// 同一类设置合并存储在一条记录中，value 为 JSON，便于后续扩展。
package settings

const (
	// KeyLanguage 界面语言，与 i18n 服务共用同一个键，保持单值存储。
	KeyLanguage = "language"

	// KeyCrawler 爬虫设置分组，value 为 JSON：{"enabled":bool,"port":int}
	KeyCrawler = "crawler"
	// KeyDownload 下载设置分组，value 为 JSON：{"dir":"D:\\Downloads"}
	KeyDownload = "download"
	// KeyUI 界面设置分组，value 为 JSON：
	// {"themeMode":"dark|light","accentColor":"#RRGGBB","pageSize":int,"sidebarWidth":int}
	KeyUI = "ui"

	// 以下为设置页与各业务页面使用的逻辑设置键，不直接作为存储行名。
	// KeyCrawlerEnabled 启动时是否自动拉起 DHT 爬虫。
	KeyCrawlerEnabled = "crawler.enabled"
	// KeyCrawlerPort DHT 爬虫监听的 UDP 端口。
	KeyCrawlerPort = "crawler.port"
	// KeyDownloadDir 磁力任务下载文件的保存目录，对之后新建的任务生效。
	KeyDownloadDir = "download.dir"
	// KeyThemeMode 界面主题模式，dark 或 light。
	KeyThemeMode = "ui.theme_mode"
	// KeyAccentColor 界面强调色。
	KeyAccentColor = "ui.accent_color"
	// KeyResourcePageSize 种子资源列表默认每页条数，由资源页就地保存。
	KeyResourcePageSize = "ui.resource_page_size"
	// KeySidebarWidth 播放页左侧播放列表宽度（像素），由播放页拖动调整后保存。
	KeySidebarWidth = "ui.sidebar_width"
)
