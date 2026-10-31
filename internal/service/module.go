package service

import (
	"github.com/bing/bing-play/internal/dhtcrawler"
	"github.com/bing/bing-play/internal/torrentmgr"
	"github.com/wailsapp/wails/v3/pkg/application"
	"go.uber.org/fx"
)

type ServiceOut struct {
	fx.Out

	Service application.Service `group:"wails-services"`
}

// provideCrawlerReloader 把 DHT 爬虫服务适配为设置变更后立即生效的接口。
func provideCrawlerReloader(s *dhtcrawler.Service) CrawlerReloader {
	return s
}

// provideDownloadDirApplier 把下载管理器适配为设置变更后立即生效的接口。
func provideDownloadDirApplier(m *torrentmgr.TorrentManager) DownloadDirApplier {
	return m
}

var Module = fx.Module(
	"service",

	fx.Provide(
		provideCrawlerReloader,
		provideDownloadDirApplier,

		NewTorrentService,
		NewTorrentWailsService,

		NewGreetService,
		NewGreetWailsService,

		NewVideoService,
		NewVideoWailsService,

		NewResourceService,
		NewResourceWailsService,

		NewSettingsService,
		NewSettingsWailsService,

		NewI18nWailsService,
	),
)
