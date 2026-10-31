package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/i18n"
	"github.com/bing/bing-play/internal/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	settingCategoryGeneral   = "general"
	settingCategoryInterface = "interface"

	settingTypeSelect = "select"
	settingTypeSwitch = "switch"
	settingTypeNumber = "number"
	settingTypeColor  = "color"
	settingTypeText   = "text"
)

// SettingOption 下拉选项。Label 为直接展示的文本（例如语言的原生名称）。
type SettingOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// SettingItem 单个设置项的元数据与当前值。
type SettingItem struct {
	Key          string          `json:"key"`
	Category     string          `json:"category"`
	Type         string          `json:"type"`
	Label        string          `json:"label"`
	Help         string          `json:"help"`
	Value        string          `json:"value"`
	DefaultValue string          `json:"defaultValue"`
	Options      []SettingOption `json:"options"`
	Min          int             `json:"min"`
	Max          int             `json:"max"`
	Step         int             `json:"step"`
	// DependsOnKey 非空时，只有该设置项的当前值等于 DependsOnValue 才在设置页展示。
	DependsOnKey   string `json:"dependsOnKey,omitempty"`
	DependsOnValue string `json:"dependsOnValue,omitempty"`
	// Hidden 为 true 表示该设置不在设置页展示，由对应业务页面就地保存。
	Hidden bool `json:"hidden,omitempty"`
}

// SettingCategory 设置分组。
type SettingCategory struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// SettingsSnapshot 设置页所需的全部数据。
type SettingsSnapshot struct {
	Categories []SettingCategory `json:"categories"`
	Items      []SettingItem     `json:"items"`
}

// SettingValidationError 单个设置项的校验失败信息，Message 为 i18n key。
type SettingValidationError struct {
	Key     string `json:"key"`
	Message string `json:"message"`
}

// SettingsSaveResult 保存结果；校验失败时 Success 为 false 并返回错误列表。
type SettingsSaveResult struct {
	Success  bool                     `json:"success"`
	Errors   []SettingValidationError `json:"errors"`
	Snapshot *SettingsSnapshot        `json:"snapshot"`
}

var accentColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// settingDef 内部设置定义：逻辑键 -> 存储分组字段 的映射。
type settingDef struct {
	key            string
	category       string
	typ            string
	label          string
	help           string
	defaultValue   string
	options        []SettingOption
	min            int
	max            int
	step           int
	group          string
	field          string
	valueType      string
	dependsOnKey   string
	dependsOnValue string
	hidden         bool
}

// CrawlerReloader 在爬虫设置保存后立即重启爬虫，使配置在当前运行环境生效。
type CrawlerReloader interface {
	ReloadCrawler()
}

// DownloadDirApplier 在下载目录变更后更新运行时下载目录，后续新任务使用新目录。
type DownloadDirApplier interface {
	SetDownloadDir(dir string)
}

// SettingsService 系统设置服务：
//  1. 汇总所有可配置项及其默认值、控件类型
//  2. 读取 app_settings（同组设置解析 JSON）并回显
//  3. 保存前校验、按分组事务写入、重置为默认值
//  4. 爬虫配置变更后立即重启爬虫
//  5. 下载目录变更后立即更新运行时下载目录
type SettingsService struct {
	settingDAO *dao.AppSettingDAO
	crawler    CrawlerReloader
	download   DownloadDirApplier
	// defaultDownloadDir 应用初始下载目录，用作设置项默认值
	defaultDownloadDir string
}

// NewSettingsService 创建 SettingsService。
func NewSettingsService(settingDAO *dao.AppSettingDAO, crawler CrawlerReloader, cfg *config.Config, download DownloadDirApplier) *SettingsService {
	s := &SettingsService{
		settingDAO: settingDAO,
		crawler:    crawler,
		download:   download,
	}
	if cfg != nil {
		s.defaultDownloadDir = absPath(cfg.DownloadDir)
	}
	// 启动时把已保存的下载目录应用到运行时，避免重启后回到默认目录
	s.applyStoredDownloadDir()
	return s
}

// applyStoredDownloadDir 启动阶段读取已保存的下载目录并更新运行时。
func (s *SettingsService) applyStoredDownloadDir() {
	if s.download == nil {
		return
	}
	raw, err := s.settingDAO.Get(settings.KeyDownload)
	if err != nil || raw == "" {
		return
	}
	var download struct {
		Dir string `json:"dir"`
	}
	if err := json.Unmarshal([]byte(raw), &download); err != nil {
		return
	}
	if dir := strings.TrimSpace(download.Dir); dir != "" {
		s.download.SetDownloadDir(dir)
	}
}

// NewSettingsWailsService 把设置服务暴露给前端。
func NewSettingsWailsService(s *SettingsService) ServiceOut {
	return ServiceOut{
		Service: application.NewService(s),
	}
}

// GetSettings 读取已保存的配置，未保存的项使用默认值。
func (s *SettingsService) GetSettings() (*SettingsSnapshot, error) {
	stored, err := s.settingDAO.GetAll()
	if err != nil {
		return nil, err
	}

	return s.buildSnapshot(stored), nil
}

// SaveSettings 校验并保存设置，全部通过后按分组事务写入。
func (s *SettingsService) SaveSettings(values map[string]string) (*SettingsSaveResult, error) {
	defs := settingDefs(s.defaultDownloadDir)
	known := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		known[def.key] = struct{}{}
	}

	stored, err := s.settingDAO.GetAll()
	if err != nil {
		return nil, err
	}

	base := readSettingValues(stored, defs)
	merged := make(map[string]string, len(defs))
	for _, def := range defs {
		value, provided := values[def.key]
		if !provided || value == "" {
			// 未提交的项沿用已保存值，已保存值非法时回落到默认值
			if old, ok := base[def.key]; ok && old != "" && validateSetting(def, old) == "" {
				value = old
			} else {
				value = def.defaultValue
			}
		}
		if def.typ == settingTypeText {
			value = strings.TrimSpace(value)
		}
		if def.key == settings.KeyDownloadDir && value != "" {
			value = absPath(value)
		}
		merged[def.key] = value
	}

	var validationErrors []SettingValidationError
	for _, def := range defs {
		if message := validateSetting(def, merged[def.key]); message != "" {
			validationErrors = append(validationErrors, SettingValidationError{
				Key:     def.key,
				Message: message,
			})
		}
	}
	for key := range values {
		if _, ok := known[key]; !ok {
			validationErrors = append(validationErrors, SettingValidationError{
				Key:     key,
				Message: "settings.error.unknown",
			})
		}
	}

	if len(validationErrors) > 0 {
		return &SettingsSaveResult{Success: false, Errors: validationErrors}, nil
	}

	// 校验通过后确保下载目录真实可用，避免保存了无法写入的路径
	if dir := merged[settings.KeyDownloadDir]; dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return &SettingsSaveResult{Success: false, Errors: []SettingValidationError{{
				Key:     settings.KeyDownloadDir,
				Message: "settings.error.download_dir",
			}}}, nil
		}
	}

	rows, err := buildStorageRows(defs, merged)
	if err != nil {
		return nil, err
	}
	if err := s.settingDAO.SetMany(rows); err != nil {
		return nil, err
	}

	// 爬虫配置变更后立即重启，使新配置在当前运行环境生效
	s.reloadCrawlerIfChanged(base, merged, values)
	// 下载目录变更后立即更新运行时目录，后续新任务使用新路径
	s.applyDownloadDirIfChanged(base, merged, values)

	snapshot, err := s.GetSettings()
	if err != nil {
		return nil, err
	}

	return &SettingsSaveResult{Success: true, Snapshot: snapshot}, nil
}

// ResetSettings 把所有设置项写回默认值。
func (s *SettingsService) ResetSettings() (*SettingsSnapshot, error) {
	defs := settingDefs(s.defaultDownloadDir)

	stored, err := s.settingDAO.GetAll()
	if err != nil {
		return nil, err
	}
	before := readSettingValues(stored, defs)

	defaults := make(map[string]string)
	for _, def := range defs {
		defaults[def.key] = def.defaultValue
	}

	rows, err := buildStorageRows(defs, defaults)
	if err != nil {
		return nil, err
	}
	if err := s.settingDAO.SetMany(rows); err != nil {
		return nil, err
	}

	// 重置会把爬虫配置改回默认值，同样需要立即生效
	submitted := map[string]string{
		settings.KeyCrawlerEnabled: defaults[settings.KeyCrawlerEnabled],
		settings.KeyCrawlerPort:    defaults[settings.KeyCrawlerPort],
		settings.KeyDownloadDir:    defaults[settings.KeyDownloadDir],
	}
	s.reloadCrawlerIfChanged(before, defaults, submitted)
	s.applyDownloadDirIfChanged(before, defaults, submitted)

	return s.GetSettings()
}

// applyDownloadDirIfChanged 在提交了下载目录且值发生变化（或此前没有存储值）时更新运行时目录。
func (s *SettingsService) applyDownloadDirIfChanged(before, after, submitted map[string]string) {
	if s.download == nil {
		return
	}
	if _, ok := submitted[settings.KeyDownloadDir]; !ok {
		return
	}
	dir := after[settings.KeyDownloadDir]
	if dir == "" {
		return
	}
	if old, exists := before[settings.KeyDownloadDir]; exists && old == dir {
		return
	}
	s.download.SetDownloadDir(dir)
}

// reloadCrawlerIfChanged 在提交了爬虫配置且值发生变化（或此前没有存储值）时重启爬虫。
func (s *SettingsService) reloadCrawlerIfChanged(before, after, submitted map[string]string) {
	if s.crawler == nil {
		return
	}
	if _, ok := submitted[settings.KeyCrawlerEnabled]; !ok {
		if _, ok := submitted[settings.KeyCrawlerPort]; !ok {
			return
		}
	}

	for _, key := range []string{settings.KeyCrawlerEnabled, settings.KeyCrawlerPort} {
		old, exists := before[key]
		if !exists || old == "" || old != after[key] {
			s.crawler.ReloadCrawler()
			return
		}
	}
}

// buildSnapshot 合并已保存值与默认值，非法的已保存值回落到默认值。
func (s *SettingsService) buildSnapshot(stored map[string]string) *SettingsSnapshot {
	defs := settingDefs(s.defaultDownloadDir)
	storedValues := readSettingValues(stored, defs)

	items := make([]SettingItem, 0, len(defs))
	for _, def := range defs {
		item := SettingItem{
			Key:            def.key,
			Category:       def.category,
			Type:           def.typ,
			Label:          def.label,
			Help:           def.help,
			Value:          def.defaultValue,
			DefaultValue:   def.defaultValue,
			Options:        def.options,
			Min:            def.min,
			Max:            def.max,
			Step:           def.step,
			DependsOnKey:   def.dependsOnKey,
			DependsOnValue: def.dependsOnValue,
			Hidden:         def.hidden,
		}
		if value, ok := storedValues[def.key]; ok && value != "" && validateSetting(def, value) == "" {
			item.Value = value
		}
		items = append(items, item)
	}

	return &SettingsSnapshot{
		Categories: []SettingCategory{
			{Key: settingCategoryGeneral, Title: "settings.category.general"},
			{Key: settingCategoryInterface, Title: "settings.category.interface"},
		},
		Items: items,
	}
}

// settingDefs 定义全部设置项。同组设置（crawler / download / ui）共用一条 JSON 记录。
func settingDefs(defaultDownloadDir string) []settingDef {
	return []settingDef{
		{
			key:          settings.KeyLanguage,
			category:     settingCategoryGeneral,
			typ:          settingTypeSelect,
			label:        "settings.item.language",
			help:         "settings.help.language",
			defaultValue: "en",
			options:      loadLanguageOptions(),
			valueType:    "string",
		},
		{
			key:          settings.KeyDownloadDir,
			category:     settingCategoryGeneral,
			typ:          settingTypeText,
			label:        "settings.item.download_dir",
			help:         "settings.help.download_dir",
			defaultValue: defaultDownloadDir,
			group:        settings.KeyDownload,
			field:        "dir",
			valueType:    "string",
		},
		{
			key:            settings.KeyCrawlerEnabled,
			category:       settingCategoryGeneral,
			typ:            settingTypeSwitch,
			label:          "settings.item.crawler_enabled",
			help:           "settings.help.crawler_enabled",
			defaultValue:   "true",
			group:          settings.KeyCrawler,
			field:          "enabled",
			valueType:      "bool",
			dependsOnKey:   "",
			dependsOnValue: "",
		},
		{
			key:            settings.KeyCrawlerPort,
			category:       settingCategoryGeneral,
			typ:            settingTypeNumber,
			label:          "settings.item.crawler_port",
			help:           "settings.help.crawler_port",
			defaultValue:   "51414",
			min:            1024,
			max:            65535,
			step:           1,
			group:          settings.KeyCrawler,
			field:          "port",
			valueType:      "int",
			dependsOnKey:   settings.KeyCrawlerEnabled,
			dependsOnValue: "true",
		},
		{
			key:          settings.KeyThemeMode,
			category:     settingCategoryInterface,
			typ:          settingTypeSelect,
			label:        "settings.item.theme_mode",
			help:         "settings.help.theme_mode",
			defaultValue: "dark",
			options: []SettingOption{
				{Label: "settings.theme.dark", Value: "dark"},
				{Label: "settings.theme.light", Value: "light"},
			},
			group:     settings.KeyUI,
			field:     "themeMode",
			valueType: "string",
		},
		{
			key:          settings.KeyAccentColor,
			category:     settingCategoryInterface,
			typ:          settingTypeColor,
			label:        "settings.item.accent_color",
			help:         "settings.help.accent_color",
			defaultValue: "#3b82f6",
			group:        settings.KeyUI,
			field:        "accentColor",
			valueType:    "string",
		},
		{
			// 资源页分页选择时就地保存，不在设置页展示
			key:          settings.KeyResourcePageSize,
			typ:          settingTypeSelect,
			defaultValue: "100",
			options: []SettingOption{
				{Label: "50", Value: "50"},
				{Label: "100", Value: "100"},
				{Label: "200", Value: "200"},
			},
			group:     settings.KeyUI,
			field:     "pageSize",
			valueType: "int",
			hidden:    true,
		},
		{
			// 播放页拖动播放列表时就地保存，不在设置页展示
			key:          settings.KeySidebarWidth,
			typ:          settingTypeNumber,
			defaultValue: "260",
			min:          200,
			max:          360,
			step:         1,
			group:        settings.KeyUI,
			field:        "sidebarWidth",
			valueType:    "int",
			hidden:       true,
		},
	}
}

// readSettingValues 从存储行解析各逻辑设置值，分组行缺失时兼容旧的扁平键。
func readSettingValues(stored map[string]string, defs []settingDef) map[string]string {
	values := make(map[string]string, len(defs))
	groups := make(map[string]map[string]json.RawMessage)

	for _, def := range defs {
		if def.group == "" {
			if value, ok := stored[def.key]; ok && value != "" {
				values[def.key] = value
			}
			continue
		}

		fields, ok := groups[def.group]
		if !ok {
			fields = make(map[string]json.RawMessage)
			if raw, exists := stored[def.group]; exists && raw != "" {
				_ = json.Unmarshal([]byte(raw), &fields)
			}
			groups[def.group] = fields
		}

		if raw, exists := fields[def.field]; exists {
			if value := rawToString(raw); value != "" {
				values[def.key] = value
				continue
			}
		}
		// 兼容旧版本分散存储的扁平键（例如 ui.accent_color）
		if value, exists := stored[def.key]; exists && value != "" {
			values[def.key] = value
		}
	}

	return values
}

// rawToString 把 JSON 原始值转换为设置页使用的字符串。
func rawToString(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}

	var boolean bool
	if err := json.Unmarshal(raw, &boolean); err == nil {
		return strconv.FormatBool(boolean)
	}

	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return strconv.FormatInt(number, 10)
	}

	return ""
}

// buildStorageRows 把逻辑设置值转换为待持久化的行：单值键直接写，分组键写 JSON。
func buildStorageRows(defs []settingDef, values map[string]string) (map[string]string, error) {
	rows := make(map[string]string)
	groups := make(map[string]map[string]any)

	for _, def := range defs {
		value := values[def.key]
		if def.group == "" {
			rows[def.key] = value
			continue
		}

		fields, ok := groups[def.group]
		if !ok {
			fields = make(map[string]any)
			groups[def.group] = fields
		}

		switch def.valueType {
		case "bool":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return nil, err
			}
			fields[def.field] = parsed
		case "int":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			fields[def.field] = parsed
		default:
			fields[def.field] = value
		}
	}

	for group, fields := range groups {
		raw, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		rows[group] = string(raw)
	}

	return rows, nil
}

// validateSetting 校验单个设置值，返回空字符串表示通过，否则返回 i18n 错误 key。
func validateSetting(def settingDef, value string) string {
	switch def.key {
	case settings.KeyLanguage:
		for _, option := range def.options {
			if option.Value == value {
				return ""
			}
		}
		return "settings.error.language"

	case settings.KeyDownloadDir:
		return validateDownloadDir(value)

	case settings.KeyCrawlerEnabled:
		if _, err := strconv.ParseBool(value); err != nil {
			return "settings.error.boolean"
		}

	case settings.KeyCrawlerPort:
		port, err := strconv.Atoi(value)
		if err != nil || port < 1024 || port > 65535 {
			return "settings.error.port"
		}

	case settings.KeyResourcePageSize:
		if value != "50" && value != "100" && value != "200" {
			return "settings.error.page_size"
		}

	case settings.KeyThemeMode:
		for _, option := range def.options {
			if option.Value == value {
				return ""
			}
		}
		return "settings.error.theme_mode"

	case settings.KeyAccentColor:
		if !accentColorPattern.MatchString(value) {
			return "settings.error.color"
		}

	case settings.KeySidebarWidth:
		width, err := strconv.Atoi(value)
		if err != nil || width < 200 || width > 360 {
			return "settings.error.sidebar_width"
		}
	}

	return ""
}

// validateDownloadDir 校验下载目录：必须非空，已存在时必须是目录；不存在的路径允许保存后创建。
func validateDownloadDir(value string) string {
	dir := strings.TrimSpace(value)
	if dir == "" {
		return "settings.error.download_dir"
	}
	info, err := os.Stat(dir)
	if err == nil {
		if info.IsDir() {
			return ""
		}
		return "settings.error.download_dir"
	}
	if os.IsNotExist(err) {
		return ""
	}
	return "settings.error.download_dir"
}

// absPath 返回路径的绝对地址，转换失败时返回原值。
func absPath(value string) string {
	dir := strings.TrimSpace(value)
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

// ValidateDownloadDir 校验下载目录是否可用，合法时返回空字符串，否则返回国际化错误 key。
func (s *SettingsService) ValidateDownloadDir(dir string) (string, error) {
	return validateDownloadDir(dir), nil
}

// SelectDownloadDir 打开系统目录选择器，返回所选目录的绝对路径；用户取消时返回空字符串。
func (s *SettingsService) SelectDownloadDir(title string, current string) (string, error) {
	app := application.Get()
	if app == nil {
		return "", errors.New("application is not ready")
	}

	initial := absPath(current)
	if initial == "" {
		initial = s.defaultDownloadDir
	}

	selected, err := app.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		CanChooseDirectories: true,
		CanChooseFiles:      false,
		CanCreateDirectories: true,
		ResolvesAliases:      true,
		Title:                title,
		Directory:            initial,
	}).PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return "", nil
	}
	return absPath(selected), nil
}

// loadLanguageOptions 从语言目录读取可选语言，读取失败时使用内置兜底列表。
func loadLanguageOptions() []SettingOption {
	entries, err := os.ReadDir(i18n.LocaleDir)
	if err != nil {
		return fallbackLanguageOptions()
	}

	options := make([]SettingOption, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "active.") || !strings.HasSuffix(name, ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(i18n.LocaleDir, name))
		if err != nil {
			continue
		}

		var content struct {
			Locale struct {
				Code       string `json:"code"`
				NativeName string `json:"nativeName"`
			} `json:"locale"`
		}
		if err := json.Unmarshal(data, &content); err != nil {
			continue
		}
		if content.Locale.Code == "" || content.Locale.NativeName == "" {
			continue
		}

		options = append(options, SettingOption{
			Label: content.Locale.NativeName,
			Value: content.Locale.Code,
		})
	}

	if len(options) == 0 {
		return fallbackLanguageOptions()
	}
	return options
}

func fallbackLanguageOptions() []SettingOption {
	return []SettingOption{
		{Label: "English", Value: "en"},
		{Label: "简体中文", Value: "zh-CN"},
		{Label: "繁體中文", Value: "zh-TW"},
	}
}
