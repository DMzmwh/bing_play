package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bing/bing-play/internal/assets"
	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/logger"
	i18nlib "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const (
	// localeSubDir 用户扩展语言包在数据目录下的相对路径。
	localeSubDir     = "resources/locales"
	SettingKeyLanguage = "language"
)

// PlayerLocaleKey ArtPlayer 自身文案在语言文件中的分组名，其内容直接使用 ArtPlayer 的原生 key
const PlayerLocaleKey = "art.player"

type I18nService struct {
	bundle      *i18nlib.Bundle
	lang        string
	defaultLang string
	settingDAO  *dao.AppSettingDAO
	resFS       assets.FS
	// localeDir 用户扩展语言包目录，位于用户数据目录下，安装目录通常不可写。
	localeDir string
	mu        sync.RWMutex
}

type LocaleInfo struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NativeName string `json:"nativeName"`
	Filename   string `json:"filename"`
}

func NewI18nService(settingDAO *dao.AppSettingDAO, resFS assets.FS, cfg *config.Config) (*I18nService, error) {
	defaultLang := "en"
	localeDir := localeSubDir
	if cfg != nil && cfg.DataDir != "" {
		localeDir = filepath.Join(cfg.DataDir, localeSubDir)
	}
	bundle := i18nlib.NewBundle(
		language.English,
	)

	savedLang, _ := settingDAO.Get(SettingKeyLanguage)
	if savedLang == "" {
		// 首次启动跟随系统界面语言，识别失败时回退默认语言
		savedLang = detectSystemLang()
		if savedLang == "" {
			savedLang = defaultLang
		}
	}
	//bundle.RegisterUnmarshalFunc(
	//	"toml",
	//	toml.Unmarshal,
	//)

	// 注册 json 解析器（替换 toml.Unmarshal）
	bundle.RegisterUnmarshalFunc(
		"json",
		json.Unmarshal,
	)
	err := loadLocales(
		bundle,
		resFS,
		localeDir,
	)

	if err != nil {
		return nil, err
	}

	return &I18nService{
		bundle:      bundle,
		defaultLang: defaultLang,
		lang:        savedLang,
		settingDAO:  settingDAO,
		resFS:       resFS,
		localeDir:   localeDir,
	}, nil
}

// loadLocales 先加载内置语言包，再加载用户数据目录下的同名/新增文件。
// 外部文件后加载，因此可以覆盖内置文案或新增语言；缺失的消息仍由内置文件兜底。
func loadLocales(bundle *i18nlib.Bundle, resFS assets.FS, localeDir string) error {
	entries, err := fs.ReadDir(resFS, assets.LocalesDir)
	if err != nil {
		return fmt.Errorf("read embedded locales: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !isLocaleFile(entry.Name()) {
			continue
		}
		if _, err := bundle.LoadMessageFileFS(resFS, pathpkg.Join(assets.LocalesDir, entry.Name())); err != nil {
			return err
		}
	}

	// 外部语言包可选，不存在时只使用内置版本。
	files, err := os.ReadDir(localeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read locales: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || !isLocaleFile(file.Name()) {
			continue
		}
		if _, err := bundle.LoadMessageFile(filepath.Join(localeDir, file.Name())); err != nil {
			return err
		}
	}
	return nil
}

// isLocaleFile 判断是否为可加载的语言包；导出的模板文件名以 .example.json 结尾，不参与加载。
func isLocaleFile(name string) bool {
	return pathpkg.Ext(name) == ".json" && !strings.HasSuffix(name, ".example.json")
}

func (s *I18nService) T(id string, data map[string]interface{}) string {
	s.mu.RLock()
	lang := s.lang
	s.mu.RUnlock()
	localizer := i18nlib.NewLocalizer(
		s.bundle,
		lang,
	)
	msg, err := localizer.Localize(
		&i18nlib.LocalizeConfig{
			MessageID:    id,
			TemplateData: data,
		},
	)
	if err != nil {
		return id
	}
	return msg
}

// GetCurrentLang 返回当前生效的语言代码，供前端初始化语言选择器使用。
func (s *I18nService) GetCurrentLang() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lang
}

func (s *I18nService) SetLang(lang string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Println("语言-----" + lang)

	// 判断语言是否存在
	tag, err := language.Parse(lang)
	if err != nil {
		fmt.Println("语言---2--" + lang)
		s.lang = s.defaultLang
		return
	}
	// bundle 是否加载了该语言
	if len(s.bundle.LanguageTags()) == 0 {
		s.lang = s.defaultLang
		return
	}
	found := false
	for _, t := range s.bundle.LanguageTags() {
		if t == tag {
			found = true
			break
		}
	}
	if !found {
		s.lang = s.defaultLang
		return
	}

	s.lang = lang
	if err := s.settingDAO.Set(SettingKeyLanguage, lang); err != nil {
		logger.Error.Error("保存语言设置失败", slog.Any("err", err))
	}
}

// localeFilenames 返回内置与外部语言文件的并集，按文件名去重并排序。
// onlyActive 为 true 时只返回 active.*.json。
func (s *I18nService) localeFilenames(onlyActive bool) ([]string, error) {
	merged := make(map[string]struct{})

	entries, err := fs.ReadDir(s.resFS, assets.LocalesDir)
	if err != nil {
		return nil, fmt.Errorf("read embedded locales: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isLocaleFile(name) {
			continue
		}
		if onlyActive && !strings.HasPrefix(name, "active.") {
			continue
		}
		merged[name] = struct{}{}
	}

	files, err := os.ReadDir(s.localeDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read locales: %w", err)
	}
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !isLocaleFile(name) {
			continue
		}
		if onlyActive && !strings.HasPrefix(name, "active.") {
			continue
		}
		merged[name] = struct{}{}
	}

	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// readLocaleFile 读取语言文件：用户数据目录下的文件优先，不存在时回退到内置版本。
func (s *I18nService) readLocaleFile(filename string) ([]byte, error) {
	if filename == "" || filename != filepath.Base(filename) {
		return nil, fmt.Errorf("非法文件路径")
	}

	data, err := os.ReadFile(filepath.Join(s.localeDir, filename))
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	return fs.ReadFile(s.resFS, pathpkg.Join(assets.LocalesDir, filename))
}

// GetLangList 获取全部 json 语言文件名（内置 + 外部）。
func (s *I18nService) GetLangList() ([]string, error) {
	return s.localeFilenames(false)
}

func (s *I18nService) GetLangListV2() ([]LocaleInfo, error) {
	files, err := s.localeFilenames(true)
	if err != nil {
		return nil, err
	}

	locales := make([]LocaleInfo, 0, len(files))
	for _, file := range files {
		data, err := s.readLocaleFile(file)
		if err != nil {
			return nil, fmt.Errorf("读取语言文件 %s: %w", file, err)
		}

		var content struct {
			Locale LocaleInfo `json:"locale"`
		}
		if err := json.Unmarshal(data, &content); err != nil {
			return nil, fmt.Errorf("解析语言文件 %s: %w", file, err)
		}

		info := content.Locale
		if info.Code == "" || info.NativeName == "" {
			continue
		}

		if _, err := language.Parse(info.Code); err != nil {
			continue
		}

		info.Filename = file
		locales = append(locales, info)
	}

	return locales, nil
}

// LoadLangFileToFlat 读取go-i18n嵌套json，转为扁平json字符串返回给vue-i18n。
// 外部语言文件优先，缺失时回退到内置版本。
func (s *I18nService) LoadLangFileToFlat(filename string) (string, error) {
	data, err := s.readLocaleFile(filename)
	if err != nil {
		return "", err
	}

	flat, err := loadFlatData(data)
	if err != nil {
		return "", err
	}

	flatData, err := json.Marshal(flat)
	if err != nil {
		return "", err
	}
	return string(flatData), nil
}

// ExportLocaleTemplate 把当前语言的内置语言包导出为模板文件，供用户参考格式并扩展翻译。
// 文件写到用户数据目录的 resources/locales 下，文件名形如 active.zh-CN.example.json。
func (s *I18nService) ExportLocaleTemplate() (string, error) {
	s.mu.RLock()
	lang := s.lang
	s.mu.RUnlock()

	lang = strings.TrimSpace(lang)
	if lang == "" || lang != filepath.Base(lang) {
		return "", fmt.Errorf("非法的语言代码")
	}

	filename := "active." + lang + ".json"
	var data []byte
	var err error
	// 优先使用内置文件，保证导出的模板包含最新、完整的 key。
	if data, err = fs.ReadFile(s.resFS, pathpkg.Join(assets.LocalesDir, filename)); err != nil {
		if data, err = os.ReadFile(filepath.Join(s.localeDir, filename)); err != nil {
			return "", fmt.Errorf("读取语言文件 %s: %w", filename, err)
		}
	}

	var content any
	if err := json.Unmarshal(data, &content); err != nil {
		return "", fmt.Errorf("解析语言文件 %s: %w", filename, err)
	}
	pretty, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return "", fmt.Errorf("格式化语言文件 %s: %w", filename, err)
	}

	if err := os.MkdirAll(s.localeDir, 0o755); err != nil {
		return "", fmt.Errorf("创建语言目录失败: %w", err)
	}

	dest := filepath.Join(s.localeDir, "active."+lang+".example.json")
	if err := os.WriteFile(dest, append(pretty, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("写入语言模板失败: %w", err)
	}
	return dest, nil
}

// loadFlatData 把语言文件内容转为 key -> 文本 的扁平表；
// locale、art.player 这类非 go-i18n 消息结构的分组会被跳过
func loadFlatData(data []byte) (map[string]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析翻译文件失败: %w", err)
	}

	type msgItem struct {
		Other string `json:"other"`
	}
	flat := make(map[string]string, len(raw))
	for k, v := range raw {
		var item msgItem
		// 非消息结构（如 locale、art.player）解析后没有 other，直接跳过
		if err := json.Unmarshal(v, &item); err != nil || item.Other == "" {
			continue
		}
		flat[k] = item.Other
	}
	return flat, nil
}

// GetPlayerLang 返回当前语言下 ArtPlayer 自身的文案，内容直接取自语言文件的 art.player 分组
func (s *I18nService) GetPlayerLang() (map[string]string, error) {
	s.mu.RLock()
	useLang := s.lang
	s.mu.RUnlock()

	data, err := s.readLocaleFile(fmt.Sprintf("active.%s.json", useLang))
	if err != nil {
		return nil, err
	}

	var content struct {
		Player map[string]string `json:"art.player"`
	}
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("解析播放器文案失败: %w", err)
	}
	return content.Player, nil
}

// SwitchLang 统一入口：切换语言，返回扁平翻译文本给前端
func (s *I18nService) GetLangInfo() (string, error) {
	s.mu.RLock()
	useLang := s.lang
	s.mu.RUnlock()
	// ✅ 修复：传入 useLang 拼接文件名
	filename := fmt.Sprintf("active.%s.json", useLang)
	// 3. 读取对应语言文件，转为扁平JSON返回给Vue-i18n
	return s.LoadLangFileToFlat(filename)
}
