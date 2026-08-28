package service

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

const (
	// UpdaterEvent 是后台检查发现新版本后通知前端的事件。
	UpdaterEvent = "updater:update"

	// UpdaterCheckInterval 是后台自动检查更新的间隔。
	UpdaterCheckInterval = 30 * time.Minute
)

// UpdateInfo 是发送给前端的更新信息。
type UpdateInfo struct {
	Version string `json:"version"`
}

type UpdaterService struct {
	app *application.App

	repository string
	version    string

	// 防止后台检查和其他检查同时执行。
	checkMu sync.Mutex
}

// NewUpdaterService 创建 UpdaterService。
func NewUpdaterService() *UpdaterService {
	return &UpdaterService{
		repository: "wailsapp/updater-demo",
	}
}

// Init 初始化 Updater，并启动后台自动检查。
//
// app 在 Wails App 创建完成后传入。
// version 是当前应用版本号。
func (m *UpdaterService) Init(app *application.App, version string) error {
	m.app = app
	m.version = version

	gh, err := github.New(github.Config{
		Repository:    m.repository,
		ChecksumAsset: "SHA256SUMS",
	})
	if err != nil {
		return err
	}

	if err := m.app.Updater.Init(wailsupdater.Config{
		CurrentVersion: m.version,
		CheckInterval:  UpdaterCheckInterval,
		Window:         wailsupdater.WindowNone,
		Providers: []wailsupdater.Provider{
			gh,
		},
	}); err != nil {
		return err
	}

	return nil
}

//前端直接监控这个提示更新
//import { Events, Updater } from "/wails/runtime.js"
//Events.On(Updater.EventUpdateAvailable, (event) => {
//console.log("发现新版本", event)
//// 打开自己的更新弹窗
//})

// Ignore 用户选择“不更新”。
// 只停止当前应用运行期间的后台自动检查。
// 应用重新启动后会重新开始自动检查。
func (m *UpdaterService) Ignore() {
	m.app.Updater.StopPeriodicCheck()
}
