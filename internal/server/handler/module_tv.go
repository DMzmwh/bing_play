//go:build android

package handler

import "go.uber.org/fx"

// Module 在 Android TV 构建下只注册直播流代理路由。
//
// 视频点播、种子流式播放、转码相关路由（VideoHandler）在电视端不需要，
// 且其构造依赖下载引擎与转码调度器，故不在此注册。
// 桌面端的完整 Module 见同目录下的 module.go（//go:build !android）。
var Module = fx.Module(
	"handler",
	fx.Provide(
		NewTVProxyHandler,
		TVProxyRoute,
	),
)
