package utils

import (
	"os"
	"path/filepath"
)

func GetDataDir(appName string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	return filepath.Join(
		dir,
		appName,
	)
}
