package updater

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

const (
	// CheckInterval 是后台自动检查更新的间隔。
	CheckInterval = 30 * time.Minute
	// Repository 是 GitHub 更新仓库。
	Repository = "wailsapp/updater-demo"
	// ChecksumAsset 是 Release 中的校验文件。
	ChecksumAsset = "SHA256SUMS"
)

// Init 初始化 Wails Updater。
func Init(app *application.App, version string) error {
	gh, err := github.New(github.Config{
		Repository:    Repository,
		ChecksumAsset: ChecksumAsset,
	})
	if err != nil {
		return err
	}

	return app.Updater.Init(wailsupdater.Config{
		CurrentVersion: version,
		// 后台自动检查。
		CheckInterval: CheckInterval,
		// 不使用 Wails 自带更新窗口。
		// 更新 UI 完全由前端控制。
		Window: wailsupdater.WindowNone,
		Providers: []wailsupdater.Provider{
			gh,
		},
	})
}
