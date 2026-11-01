package config

type Config struct {
	DataDir     string
	AppName     string
	DBPath      string
	DownloadDir string

	// ResourceDir 是 resources 目录，爬虫数据也落在这里。
	ResourceDir string
	// CrawlerEnabled 控制启动时是否自动拉起 DHT 爬虫。
	CrawlerEnabled bool
	// CrawlerPort 是爬虫 DHT 使用的 UDP 端口，需与 torrentmgr 的端口错开。
	CrawlerPort int

	// ServerAddr 是本地 HTTP 服务的固定监听地址。
	// 前端 Play.vue 的视频流地址与此保持一致，固定端口避免端口漂移后前后端失联。
	ServerAddr string
}
