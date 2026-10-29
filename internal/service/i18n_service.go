package service

import (
	"github.com/bing/bing-play/internal/i18n"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// NewI18nWailsService 把 i18n 服务暴露给前端。
// 前端所有提示文本都通过它获取，不在前端硬编码文案。
func NewI18nWailsService(i18nService *i18n.I18nService) ServiceOut {
	return ServiceOut{
		Service: application.NewService(i18nService),
	}
}
