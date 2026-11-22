//go:build android

package config

import (
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Load 返回 Android/TV 运行配置。
//
// Android 应用的进程工作目录是只读的 "/"，不能像桌面开发版那样使用相对路径
// "./data"，因此数据目录统一放到应用私有目录（getFilesDir）下。
func Load() *Config {
	basePath := filepath.Join(application.Android.StoragePath(), "bing-play")
	return &Config{
		AppName: "bing-play",
		DataDir: basePath,
		DBPath: filepath.Join(
			basePath,
			"db",
			"bt.db",
		),
		DownloadDir: filepath.Join(
			basePath,
			"downloads",
		),
		ResourceDir:    "resources",
		CrawlerEnabled: false,
		CrawlerPort:    51414,
		ServerAddr:     "127.0.0.1:9090",
	}
}
