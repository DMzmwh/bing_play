//go:build prod

package config

import (
	"changeme/internal/utils"
	"fmt"
	"path/filepath"
)

func Load() *Config {

	fmt.Println("启动配置 - PROD")
	basePath := utils.GetDataDir("bing-play")
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
	}
}
