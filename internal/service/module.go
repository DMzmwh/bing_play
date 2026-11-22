//go:build !android

package service

import (
	"github.com/bing/bing-play/internal/dhtcrawler"
	"github.com/bing/bing-play/internal/torrentmgr"
	"go.uber.org/fx"
)

// ServiceOut 类型已移至 serviceout.go（无 build tag），桌面端与 Android TV 端共用。

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

		NewTVService,
		NewTVWailsService,

		NewI18nWailsService,
	),
)
