package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/settings"
	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
)

// fakeCrawlerReloader 记录爬虫重启次数，用于验证设置保存后是否立即生效。
type fakeCrawlerReloader struct {
	calls int
}

func (f *fakeCrawlerReloader) ReloadCrawler() {
	f.calls++
}

// fakeDownloadDirApplier 记录运行时下载目录的更新，用于验证设置保存后是否立即生效。
type fakeDownloadDirApplier struct {
	dirs []string
}

func (f *fakeDownloadDirApplier) SetDownloadDir(dir string) {
	f.dirs = append(f.dirs, dir)
}

// newTestSettingsService 创建基于内存 SQLite 的设置服务。
func newTestSettingsService(t *testing.T) (*SettingsService, *dao.AppSettingDAO) {
	return newTestSettingsServiceWithCrawler(t, nil)
}

func newTestSettingsServiceWithCrawler(t *testing.T, crawler CrawlerReloader) (*SettingsService, *dao.AppSettingDAO) {
	s, settingDAO, _ := newTestSettingsServiceWithDeps(t, crawler, nil)
	return s, settingDAO
}

func newTestSettingsServiceWithDeps(
	t *testing.T,
	crawler CrawlerReloader,
	download DownloadDirApplier,
) (*SettingsService, *dao.AppSettingDAO, *config.Config) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	err = db.Exec(`
CREATE TABLE app_settings (
    key        TEXT NOT NULL PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`).Error
	if err != nil {
		t.Fatalf("创建 app_settings 表失败: %v", err)
	}

	settingDAO := dao.NewAppSettingDAO(db)
	cfg := &config.Config{DownloadDir: t.TempDir()}
	return NewSettingsService(settingDAO, crawler, cfg, download), settingDAO, cfg
}

func snapshotValue(t *testing.T, snapshot *SettingsSnapshot, key string) string {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Key == key {
			return item.Value
		}
	}
	t.Fatalf("快照中不存在设置项 %s", key)
	return ""
}

func snapshotItem(t *testing.T, snapshot *SettingsSnapshot, key string) SettingItem {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("快照中不存在设置项 %s", key)
	return SettingItem{}
}

func TestGetSettingsUsesDefaults(t *testing.T) {
	s, _ := newTestSettingsService(t)

	snapshot, err := s.GetSettings()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}

	// 通知与隐私分组已移除，只保留通用与界面
	if len(snapshot.Categories) != 2 {
		t.Fatalf("分类数量 = %d, want 2", len(snapshot.Categories))
	}
	// 6 个设置页可见项 + 2 个由业务页面就地保存的隐藏项
	if len(snapshot.Items) != 8 {
		t.Fatalf("设置项数量 = %d, want 8", len(snapshot.Items))
	}

	for _, item := range snapshot.Items {
		if item.Value != item.DefaultValue {
			t.Fatalf("%s = %q, want default %q", item.Key, item.Value, item.DefaultValue)
		}
	}

	if got := snapshotValue(t, snapshot, settings.KeyLanguage); got != "en" {
		t.Fatalf("language = %q, want en", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeyAccentColor); got != "#3b82f6" {
		t.Fatalf("accent color = %q, want #3b82f6", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeyThemeMode); got != "dark" {
		t.Fatalf("theme mode = %q, want dark", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeyDownloadDir); got == "" {
		t.Fatalf("下载目录默认值不应为空")
	}

	// 端口仅在爬虫开启时展示
	port := snapshotItem(t, snapshot, settings.KeyCrawlerPort)
	if port.DependsOnKey != settings.KeyCrawlerEnabled || port.DependsOnValue != "true" {
		t.Fatalf("端口依赖配置 = %s=%s", port.DependsOnKey, port.DependsOnValue)
	}

	// 每页条数与播放列表宽度不在设置页展示
	for _, key := range []string{settings.KeyResourcePageSize, settings.KeySidebarWidth} {
		if !snapshotItem(t, snapshot, key).Hidden {
			t.Fatalf("%s 应为隐藏设置项", key)
		}
	}
}

func TestSaveSettingsPersistsGroupedJSON(t *testing.T) {
	s, settingDAO := newTestSettingsService(t)

	values := map[string]string{
		settings.KeyLanguage:         "zh-CN",
		settings.KeyCrawlerEnabled:   "false",
		settings.KeyCrawlerPort:      "52000",
		settings.KeyThemeMode:        "light",
		settings.KeyAccentColor:      "#22c55e",
		settings.KeyResourcePageSize: "200",
		settings.KeySidebarWidth:     "300",
	}

	result, err := s.SaveSettings(values)
	if err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	if !result.Success {
		t.Fatalf("保存应成功，错误: %+v", result.Errors)
	}
	if result.Snapshot == nil {
		t.Fatal("保存成功后应返回最新快照")
	}

	for key, want := range values {
		if got := snapshotValue(t, result.Snapshot, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}

	// 同类型设置合并为一条 JSON 记录
	var crawler struct {
		Enabled bool `json:"enabled"`
		Port    int  `json:"port"`
	}
	raw, err := settingDAO.Get(settings.KeyCrawler)
	if err != nil {
		t.Fatalf("读取 crawler 行失败: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &crawler); err != nil {
		t.Fatalf("crawler 行不是合法 JSON: %v", err)
	}
	if crawler.Enabled || crawler.Port != 52000 {
		t.Fatalf("crawler JSON = %+v", crawler)
	}

	var ui struct {
		ThemeMode    string `json:"themeMode"`
		AccentColor  string `json:"accentColor"`
		PageSize     int    `json:"pageSize"`
		SidebarWidth int    `json:"sidebarWidth"`
	}
	raw, err = settingDAO.Get(settings.KeyUI)
	if err != nil {
		t.Fatalf("读取 ui 行失败: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &ui); err != nil {
		t.Fatalf("ui 行不是合法 JSON: %v", err)
	}
	if ui.ThemeMode != "light" || ui.AccentColor != "#22c55e" || ui.PageSize != 200 || ui.SidebarWidth != 300 {
		t.Fatalf("ui JSON = %+v", ui)
	}

	// 语言保持单值存储，兼容 i18n 服务
	lang, err := settingDAO.Get(settings.KeyLanguage)
	if err != nil {
		t.Fatalf("读取 language 行失败: %v", err)
	}
	if lang != "zh-CN" {
		t.Fatalf("language 行 = %q, want zh-CN", lang)
	}
}

func TestSaveSettingsPartialKeepsOtherValues(t *testing.T) {
	s, _ := newTestSettingsService(t)

	if _, err := s.SaveSettings(map[string]string{
		settings.KeyAccentColor: "#f97316",
		settings.KeyCrawlerPort: "52000",
	}); err != nil {
		t.Fatalf("首次保存失败: %v", err)
	}

	// 资源页只提交每页条数，其它设置应保持不变
	result, err := s.SaveSettings(map[string]string{
		settings.KeyResourcePageSize: "200",
	})
	if err != nil {
		t.Fatalf("就地保存每页条数失败: %v", err)
	}
	if !result.Success {
		t.Fatalf("就地保存应成功，错误: %+v", result.Errors)
	}

	if got := snapshotValue(t, result.Snapshot, settings.KeyResourcePageSize); got != "200" {
		t.Fatalf("page size = %q, want 200", got)
	}
	if got := snapshotValue(t, result.Snapshot, settings.KeyAccentColor); got != "#f97316" {
		t.Fatalf("accent color = %q, want 保留 #f97316", got)
	}
	if got := snapshotValue(t, result.Snapshot, settings.KeyCrawlerPort); got != "52000" {
		t.Fatalf("crawler port = %q, want 保留 52000", got)
	}
}

func TestSaveSettingsValidationFailure(t *testing.T) {
	s, settingDAO := newTestSettingsService(t)

	result, err := s.SaveSettings(map[string]string{
		settings.KeyCrawlerPort:      "80",    // 端口低于最小值
		settings.KeyAccentColor:      "red",   // 非法颜色
		settings.KeyThemeMode:        "blue",  // 非法主题
		settings.KeySidebarWidth:     "100",   // 宽度低于最小值
		settings.KeyLanguage:         "xx-YY", // 不支持的语言
		settings.KeyResourcePageSize: "20",    // 非法每页条数
	})
	if err != nil {
		t.Fatalf("校验失败不应返回系统错误: %v", err)
	}
	if result.Success {
		t.Fatal("非法输入不应保存成功")
	}

	if len(result.Errors) != 6 {
		t.Fatalf("错误数量 = %d, want 6: %+v", len(result.Errors), result.Errors)
	}
	for _, errItem := range result.Errors {
		if errItem.Message == "" {
			t.Fatalf("%s 缺少错误文案 key", errItem.Key)
		}
	}

	// 校验失败时不应写入任何数据
	stored, err := settingDAO.GetAll()
	if err != nil {
		t.Fatalf("读取存储失败: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("校验失败后不应写入数据: %v", stored)
	}
}

func TestSaveSettingsUnknownKey(t *testing.T) {
	s, _ := newTestSettingsService(t)

	result, err := s.SaveSettings(map[string]string{
		"unknown.key": "value",
	})
	if err != nil {
		t.Fatalf("未知键不应返回系统错误: %v", err)
	}
	if result.Success {
		t.Fatal("未知键不应保存成功")
	}
	if len(result.Errors) != 1 || result.Errors[0].Key != "unknown.key" {
		t.Fatalf("未知键错误 = %+v", result.Errors)
	}
	if result.Errors[0].Message != "settings.error.unknown" {
		t.Fatalf("未知键错误文案 = %q", result.Errors[0].Message)
	}
}

func TestResetSettingsRestoresDefaults(t *testing.T) {
	s, settingDAO := newTestSettingsService(t)

	if _, err := s.SaveSettings(map[string]string{
		settings.KeyLanguage:         "zh-TW",
		settings.KeyAccentColor:      "#ef4444",
		settings.KeyResourcePageSize: "50",
	}); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}

	snapshot, err := s.ResetSettings()
	if err != nil {
		t.Fatalf("重置设置失败: %v", err)
	}
	for _, item := range snapshot.Items {
		if item.Value != item.DefaultValue {
			t.Fatalf("重置后 %s = %q, want default %q", item.Key, item.Value, item.DefaultValue)
		}
	}

	var crawler struct {
		Enabled bool `json:"enabled"`
		Port    int  `json:"port"`
	}
	raw, err := settingDAO.Get(settings.KeyCrawler)
	if err != nil {
		t.Fatalf("读取 crawler 行失败: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &crawler); err != nil {
		t.Fatalf("重置后 crawler 行不是合法 JSON: %v", err)
	}
	if !crawler.Enabled || crawler.Port != 51414 {
		t.Fatalf("重置后 crawler = %+v", crawler)
	}
}

func TestGetSettingsFallsBackOnInvalidStoredValue(t *testing.T) {
	s, settingDAO := newTestSettingsService(t)

	// 直接写入非法 JSON/脏数据，模拟历史异常数据
	if err := settingDAO.SetMany(map[string]string{
		settings.KeyCrawler:  "{bad",
		settings.KeyUI:       `{"themeMode":"blue","accentColor":"not-a-color","pageSize":999,"sidebarWidth":100}`,
		settings.KeyLanguage: "unsupported",
	}); err != nil {
		t.Fatalf("写入脏数据失败: %v", err)
	}

	snapshot, err := s.GetSettings()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}

	if got := snapshotValue(t, snapshot, settings.KeyCrawlerPort); got != "51414" {
		t.Fatalf("非法爬虫配置应回落到默认值，got %q", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeyAccentColor); got != "#3b82f6" {
		t.Fatalf("非法颜色应回落到默认值，got %q", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeyThemeMode); got != "dark" {
		t.Fatalf("非法主题应回落到默认值，got %q", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeyResourcePageSize); got != "100" {
		t.Fatalf("非法每页条数应回落到默认值，got %q", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeySidebarWidth); got != "260" {
		t.Fatalf("非法播放列表宽度应回落到默认值，got %q", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeyLanguage); got != "en" {
		t.Fatalf("非法语言应回落到默认值，got %q", got)
	}
}

func TestGetSettingsReadsLegacyFlatKeys(t *testing.T) {
	s, settingDAO := newTestSettingsService(t)

	// 旧版本使用扁平键存储，升级后应继续回显
	if err := settingDAO.SetMany(map[string]string{
		settings.KeyCrawlerEnabled:   "false",
		settings.KeyCrawlerPort:      "52000",
		settings.KeyAccentColor:      "#22c55e",
		settings.KeyResourcePageSize: "200",
		settings.KeySidebarWidth:     "300",
	}); err != nil {
		t.Fatalf("写入旧格式数据失败: %v", err)
	}

	snapshot, err := s.GetSettings()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}

	if got := snapshotValue(t, snapshot, settings.KeyCrawlerEnabled); got != "false" {
		t.Fatalf("legacy crawler enabled = %q, want false", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeyCrawlerPort); got != "52000" {
		t.Fatalf("legacy crawler port = %q, want 52000", got)
	}
	if got := snapshotValue(t, snapshot, settings.KeyAccentColor); got != "#22c55e" {
		t.Fatalf("legacy accent = %q, want #22c55e", got)
	}
}

func TestSaveSettingsReloadsCrawlerImmediately(t *testing.T) {
	reloader := &fakeCrawlerReloader{}
	s, _ := newTestSettingsServiceWithCrawler(t, reloader)

	crawlerValues := map[string]string{
		settings.KeyCrawlerEnabled: "false",
		settings.KeyCrawlerPort:    "52000",
	}
	if _, err := s.SaveSettings(crawlerValues); err != nil {
		t.Fatalf("保存爬虫设置失败: %v", err)
	}
	if reloader.calls != 1 {
		t.Fatalf("爬虫配置变更后应立即重启，调用次数 = %d", reloader.calls)
	}

	// 再次提交相同配置不应重复重启
	if _, err := s.SaveSettings(crawlerValues); err != nil {
		t.Fatalf("再次保存爬虫设置失败: %v", err)
	}
	if reloader.calls != 1 {
		t.Fatalf("配置未变化时不应重启爬虫，调用次数 = %d", reloader.calls)
	}

	// 仅保存强调色不应影响爬虫运行状态
	if _, err := s.SaveSettings(map[string]string{settings.KeyAccentColor: "#22c55e"}); err != nil {
		t.Fatalf("保存强调色失败: %v", err)
	}
	if reloader.calls != 1 {
		t.Fatalf("非爬虫设置不应重启爬虫，调用次数 = %d", reloader.calls)
	}
}

func TestResetSettingsReloadsCrawler(t *testing.T) {
	reloader := &fakeCrawlerReloader{}
	s, _ := newTestSettingsServiceWithCrawler(t, reloader)

	if _, err := s.SaveSettings(map[string]string{
		settings.KeyCrawlerEnabled: "false",
		settings.KeyCrawlerPort:    "52000",
	}); err != nil {
		t.Fatalf("保存爬虫设置失败: %v", err)
	}

	if _, err := s.ResetSettings(); err != nil {
		t.Fatalf("重置设置失败: %v", err)
	}
	// 保存一次 + 重置回默认值一次
	if reloader.calls != 2 {
		t.Fatalf("重置后应立即重启爬虫，调用次数 = %d", reloader.calls)
	}
}

func TestSaveSettingsAppliesDownloadDir(t *testing.T) {
	applier := &fakeDownloadDirApplier{}
	s, settingDAO, cfg := newTestSettingsServiceWithDeps(t, nil, applier)

	target := filepath.Join(t.TempDir(), "movies")
	result, err := s.SaveSettings(map[string]string{settings.KeyDownloadDir: target})
	if err != nil {
		t.Fatalf("保存下载目录失败: %v", err)
	}
	if !result.Success {
		t.Fatalf("保存下载目录应成功: %+v", result.Errors)
	}

	// 目录应已创建，并且运行时目录立即更新
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("下载目录未创建: %v", err)
	}
	if len(applier.dirs) != 1 || applier.dirs[0] != target {
		t.Fatalf("运行时下载目录未立即更新: %+v", applier.dirs)
	}

	// 按分组 JSON 落库
	raw, err := settingDAO.Get(settings.KeyDownload)
	if err != nil {
		t.Fatalf("读取 download 行失败: %v", err)
	}
	var download struct {
		Dir string `json:"dir"`
	}
	if err := json.Unmarshal([]byte(raw), &download); err != nil {
		t.Fatalf("download 行不是合法 JSON: %v", err)
	}
	if download.Dir != target {
		t.Fatalf("download.dir = %q, want %q", download.Dir, target)
	}
	if got := snapshotValue(t, result.Snapshot, settings.KeyDownloadDir); got != target {
		t.Fatalf("下载目录回显 = %q, want %q", got, target)
	}

	// 重置后恢复默认目录，并再次应用到运行时
	snapshot, err := s.ResetSettings()
	if err != nil {
		t.Fatalf("重置设置失败: %v", err)
	}
	if got := snapshotValue(t, snapshot, settings.KeyDownloadDir); got != cfg.DownloadDir {
		t.Fatalf("重置后下载目录 = %q, want %q", got, cfg.DownloadDir)
	}
	if len(applier.dirs) != 2 || applier.dirs[1] != cfg.DownloadDir {
		t.Fatalf("重置后运行时下载目录未更新: %+v", applier.dirs)
	}
}

func TestNewSettingsServiceAppliesStoredDownloadDir(t *testing.T) {
	applier := &fakeDownloadDirApplier{}
	_, settingDAO, cfg := newTestSettingsServiceWithDeps(t, nil, applier)

	stored := filepath.Join(t.TempDir(), "stored")
	if err := settingDAO.Set(settings.KeyDownload, fmt.Sprintf(`{"dir":%q}`, stored)); err != nil {
		t.Fatalf("写入下载目录设置失败: %v", err)
	}

	// 重新构造服务模拟应用重启，启动时应立即应用已保存的目录
	_ = NewSettingsService(settingDAO, nil, cfg, applier)
	if len(applier.dirs) != 1 || applier.dirs[0] != stored {
		t.Fatalf("启动时未应用已保存的下载目录: %+v", applier.dirs)
	}
}

func TestValidateDownloadDir(t *testing.T) {
	s, _, _ := newTestSettingsServiceWithDeps(t, nil, nil)

	for _, tc := range []struct {
		name string
		dir  string
		want string
	}{
		{name: "空路径", dir: "", want: "settings.error.download_dir"},
		{name: "空白路径", dir: "   ", want: "settings.error.download_dir"},
		{name: "已存在目录", dir: t.TempDir(), want: ""},
		{name: "不存在的目录", dir: filepath.Join(t.TempDir(), "not-created"), want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ValidateDownloadDir(tc.dir)
			if err != nil {
				t.Fatalf("校验下载目录不应返回系统错误: %v", err)
			}
			if got != tc.want {
				t.Fatalf("校验结果 = %q, want %q", got, tc.want)
			}
		})
	}

	filePath := filepath.Join(t.TempDir(), "plain-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	if got, _ := s.ValidateDownloadDir(filePath); got != "settings.error.download_dir" {
		t.Fatalf("普通文件应校验失败，got %q", got)
	}
}

func TestSaveSettingsNormalizesDownloadDirToAbsolute(t *testing.T) {
	applier := &fakeDownloadDirApplier{}
	s, _, _ := newTestSettingsServiceWithDeps(t, nil, applier)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("读取工作目录失败: %v", err)
	}

	// 提交相对路径，保存后应回显、应用为绝对路径
	result, err := s.SaveSettings(map[string]string{settings.KeyDownloadDir: "."})
	if err != nil {
		t.Fatalf("保存相对下载目录失败: %v", err)
	}
	if !result.Success {
		t.Fatalf("保存相对下载目录应成功: %+v", result.Errors)
	}

	got := snapshotValue(t, result.Snapshot, settings.KeyDownloadDir)
	if got != wd {
		t.Fatalf("保存后路径 = %q, want 绝对路径 %q", got, wd)
	}
	if len(applier.dirs) != 1 || applier.dirs[0] != wd {
		t.Fatalf("运行时目录未使用绝对路径: %+v", applier.dirs)
	}
}

func TestSaveSettingsRejectsInvalidDownloadDir(t *testing.T) {
	applier := &fakeDownloadDirApplier{}
	s, _, _ := newTestSettingsServiceWithDeps(t, nil, applier)

	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}

	result, err := s.SaveSettings(map[string]string{settings.KeyDownloadDir: filePath})
	if err != nil {
		t.Fatalf("保存非法下载目录不应返回系统错误: %v", err)
	}
	if result.Success {
		t.Fatal("指向普通文件的下载目录不应保存成功")
	}
	if len(result.Errors) != 1 || result.Errors[0].Message != "settings.error.download_dir" {
		t.Fatalf("错误信息 = %+v", result.Errors)
	}
	if len(applier.dirs) != 0 {
		t.Fatalf("校验失败时不应更新运行时目录: %+v", applier.dirs)
	}
}
