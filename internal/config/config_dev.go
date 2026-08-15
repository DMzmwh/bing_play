//go:build !prod

package config

import (
	"fmt"
	"path/filepath"
)

func Load() *Config {

	fmt.Println("启动配置 - using DEV config")
	basePath := "./data"
	return &Config{
		AppName: "bing-play-dev",
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
