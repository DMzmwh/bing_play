package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	i18nlib "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const LocaleDir = "./resources/locales"

type I18nService struct {
	bundle      *i18nlib.Bundle
	lang        string
	defaultLang string
	mu          sync.RWMutex
}

type LocaleInfo struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NativeName string `json:"nativeName"`
	Filename   string `json:"filename"`
}

func NewI18nService() (*I18nService, error) {
	defaultLang := "en"
	bundle := i18nlib.NewBundle(
		language.English,
	)

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
		lang:        defaultLang,
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

func (s *I18nService) SetLang(lang string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 判断语言是否存在
	tag, err := language.Parse(lang)
	if err != nil {
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

	data, err := os.ReadFile(absTarget)
	if err != nil {
		return "", err
	}

	type msgItem struct {
		Other string `json:"other"`
	}
	var nested map[string]msgItem
	if err := json.Unmarshal(data, &nested); err != nil {
		return "", fmt.Errorf("解析翻译文件失败: %w", err)
	}

	flat := make(map[string]string)
	for k, v := range nested {
		flat[k] = v.Other
	}

	flatData, err := json.Marshal(flat)
	if err != nil {
		return "", err
	}
	return string(flatData), nil
}

// SwitchLang 统一入口：切换语言，返回扁平翻译文本给前端
func (s *I18nService) SwitchLang(lang string) (string, error) {
	// 1. 后端切换语言（内部校验，非法自动回退默认语言）
	s.SetLang(lang)
	// 2. 获取最终生效语言（防止传入不存在的语言被回退）
	s.mu.RLock()
	useLang := s.lang
	s.mu.RUnlock()
	// ✅ 修复：传入 useLang 拼接文件名
	filename := fmt.Sprintf("active.%s.json", useLang)
	// 3. 读取对应语言文件，转为扁平JSON返回给Vue-i18n
	return s.LoadLangFileToFlat(filename)
}
