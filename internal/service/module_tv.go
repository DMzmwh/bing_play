//go:build android

package service

import "go.uber.org/fx"

// Module 在 Android TV 构建下只注册电视直播与国际化服务。
//
// 不加载爬虫、下载、转码相关 provider，保证电视端：
//   - 启动时不会拉起 DHT 爬虫；
//   - 二进制不包含 BT 下载与 FFmpeg 转码代码，体积更小；
//   - Fx 依赖图里没有这些模块，避免误实例化。
//
// 桌面端的完整 Module 见同目录下的 module.go（//go:build !android）。
var Module = fx.Module(
	"service",

	fx.Provide(
		NewTVService,
		NewTVWailsService,

		NewI18nWailsService,
	),
)
