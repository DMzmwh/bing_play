package service

import (
	"changeme/internal/i18n"
	"context"
	"log"
	"sync"
	"sync/atomic"
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

// UpdateInfo 是更新信息。
type UpdateInfo struct {
	// Version 是新版本号。
	Version string `json:"version"`
}

type UpdaterService struct {
	app  *application.App
	I18n *i18n.I18nService

	repository string
	version    string

	// 防止后台检查和用户主动检查同时执行。
	checkMu sync.Mutex

	// 用户选择“不更新”后，停止本次运行期间的自动检查。
	paused atomic.Bool
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
// UpdaterService 由 Fx 创建，而 application.App
// 由 NewWailsApp 创建，因此不能在构造函数中直接注入 app。
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
		Providers: []wailsupdater.Provider{
			gh,
		},
	}); err != nil {
		return err
	}

	// 初始化成功后启动后台自动检查。
	go m.checkLoop()

	return nil
}

// checkLoop 执行后台自动检查。
//
// 启动后立即检查一次，之后每 30 分钟检查一次。
// 用户选择“不更新”后，停止本次应用运行期间的自动检查。
func (m *UpdaterService) checkLoop() {
	ticker := time.NewTicker(UpdaterCheckInterval)
	defer ticker.Stop()
	for {
		m.notifyUpdate()
		if m.paused.Load() {
			return
		}
		<-ticker.C
	}
}

// notifyUpdate 执行后台检查。
//
// 如果发现新版本，则通过 Wails Event 通知前端。
func (m *UpdaterService) notifyUpdate() {
	update, err := m.check()
	if err != nil {
		log.Printf("updater check failed: %v", err)
		return
	}

	if update == nil {
		return
	}

	m.app.Event.Emit(UpdaterEvent, update)
}

// Check 用户主动检查更新。
//
// 不发送 Event，直接返回检查结果。
//
// 有新版本：返回 UpdateInfo
// 没有新版本：返回 nil
func (m *UpdaterService) Check() (*UpdateInfo, error) {
	return m.check()
}

// check 执行一次更新检查。
//
// 这里只负责检查，不负责通知前端。
func (m *UpdaterService) check() (*UpdateInfo, error) {
	//if m.app == nil {
	//	return nil, errors.New(m.I18n.T("updater_not_initialized", nil))
	//}

	m.checkMu.Lock()
	defer m.checkMu.Unlock()

	update, err := m.app.Updater.Check(context.Background())
	if err != nil {
		return nil, err
	}

	if update == nil {
		return nil, nil
	}
	//data, err := json.Marshal(update)
	//jsonString := string(data)
	//log.Printf("updater found new version: %s", jsonString)
	return &UpdateInfo{
		Version: update.Version,
	}, nil
}

// Ignore 用户选择“不更新”。
//
// 只停止当前应用运行期间的后台自动检查。
// 应用重新启动后会重新开始自动检查。
func (m *UpdaterService) Ignore() {
	m.paused.Store(true)
}

// Update 下载并安装更新。
//
// 下载并安装完成后不会自动重启。
// 是否重启由前端决定。
func (m *UpdaterService) Update() error {

	return m.app.Updater.DownloadAndInstall(context.Background())
}

// Restart 重启应用并应用已经安装的更新。
func (m *UpdaterService) Restart() error {
	return m.app.Updater.Restart(context.Background())
}
