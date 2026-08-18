package i18n

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
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

func NewI18nService() (*I18nService, error) {
	defaultLang := "en"
	bundle := i18nlib.NewBundle(
		language.English,
	)

	bundle.RegisterUnmarshalFunc(
		"toml",
		toml.Unmarshal,
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
		if filepath.Ext(file.Name()) != ".toml" {
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
