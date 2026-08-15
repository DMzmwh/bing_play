package i18n

import (
	"embed"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed *.toml
var localeFS embed.FS

type I18nService struct {
	bundle *i18n.Bundle
	lang   string
	mu     sync.RWMutex
}

func NewI18nService(defaultLang string) (*I18nService, error) {
	bundle := i18n.NewBundle(
		language.Chinese,
	)

	bundle.RegisterUnmarshalFunc(
		"toml",
		toml.Unmarshal,
	)

	// 自动加载当前目录下所有 toml 文件
	entries, err := localeFS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf(
			"read locale dir: %w",
			err,
		)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		if filepath.Ext(name) != ".toml" {
			continue
		}

		_, err := bundle.LoadMessageFileFS(
			localeFS,
			name,
		)

		if err != nil {
			return nil, fmt.Errorf(
				"load locale %s: %w",
				name,
				err,
			)
		}
	}

	return &I18nService{
		bundle: bundle,
		lang:   defaultLang,
	}, nil
}

// SetLang 切换语言
func (s *I18nService) SetLang(lang string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lang = lang
}

// Lang 获取当前语言
func (s *I18nService) Lang() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lang
}

// T 获取翻译文本
func (s *I18nService) T(messageID string, data map[string]interface{}) string {
	s.mu.RLock()
	lang := s.lang
	s.mu.RUnlock()
	localizer := i18n.NewLocalizer(
		s.bundle,
		lang,
	)
	text, err := localizer.Localize(
		&i18n.LocalizeConfig{
			MessageID:    messageID,
			TemplateData: data,
		},
	)
	if err != nil {
		return messageID
	}
	return text
}
