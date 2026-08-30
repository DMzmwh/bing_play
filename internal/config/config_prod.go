//go:build prod

package config

import (
	"fmt"
	"github.com/bing/bing-play/internal/utils"
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
