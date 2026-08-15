//go:build prod

package config

import (
	"changeme/internal/utils"
	"fmt"
)

func Load() *Config {

	fmt.Println("启动配置 - using PROD config")
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
