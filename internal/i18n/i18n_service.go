package i18n

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/logger"
	i18nlib "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const LocaleDir = "./resources/locales"
const SettingKeyLanguage = "language"

// PlayerLocaleKey ArtPlayer 自身文案在语言文件中的分组名，其内容直接使用 ArtPlayer 的原生 key
const PlayerLocaleKey = "art.player"

type I18nService struct {
	bundle      *i18nlib.Bundle
	lang        string
	defaultLang string
	settingDAO  *dao.AppSettingDAO
	mu          sync.RWMutex
}

type LocaleInfo struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NativeName string `json:"nativeName"`
	Filename   string `json:"filename"`
}

func NewI18nService(settingDAO *dao.AppSettingDAO) (*I18nService, error) {
	defaultLang := "en"
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
		LocaleDir,
	)

	if err != nil {
		return nil, err
	}

	return &I18nService{
		bundle:      bundle,
		defaultLang: defaultLang,
		lang:        savedLang,
		settingDAO:  settingDAO,
	}, nil
}

func loadLocales(bundle *i18nlib.Bundle, dir string) error {
	files, err := os.ReadDir(dir)

	if err != nil {
		return fmt.Errorf(
			"read locales: %w",
			err,
		)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		//if filepath.Ext(file.Name()) != ".toml" {
		//	continue
		//}
		// 修改：只加载 .json 文件
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}
		_, err := bundle.LoadMessageFile(
			filepath.Join(
				dir,
				file.Name(),
			),
		)
		if err != nil {
			return err
		}
	}
	return nil
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

// GetLangList 获取locales目录下全部json语言文件名
func (s *I18nService) GetLangList() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(LocaleDir, "*.json"))
	if err != nil {
		return nil, err
	}
	var res []string
	for _, f := range files {
		res = append(res, filepath.Base(f))
	}
	return res, nil
}

func (s *I18nService) GetLangListV2() ([]LocaleInfo, error) {
	files, err := filepath.Glob(filepath.Join(LocaleDir, "active.*.json"))
	if err != nil {
		return nil, err
	}

	locales := make([]LocaleInfo, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
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

		info.Filename = filepath.Base(file)
		locales = append(locales, info)
	}

	return locales, nil
}

// LoadLangFileToFlat 读取go-i18n嵌套json，转为扁平json字符串返回给vue-i18n
func (s *I18nService) LoadLangFileToFlat(filename string) (string, error) {
	dir := LocaleDir
	fullPath := filepath.Join(dir, filename)

	// 防路径穿越
	absTarget, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if filepath.Dir(absTarget) != absDir {
		return "", fmt.Errorf("非法文件路径")
	}

	flat, err := loadFlatFile(absTarget)
	if err != nil {
		return "", err
	}

	flatData, err := json.Marshal(flat)
	if err != nil {
		return "", err
	}
	return string(flatData), nil
}

// loadFlatFile 读取单个语言文件，转为 key -> 文本 的扁平表；
// locale、art.player 这类非 go-i18n 消息结构的分组会被跳过
func loadFlatFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

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

	data, err := os.ReadFile(filepath.Join(LocaleDir, fmt.Sprintf("active.%s.json", useLang)))
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
