package service

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"go.uber.org/fx"
)

// ServiceOut 把业务服务适配为 Wails service，通过 fx group 统一收集后注册到应用。
//
// 放在无 build tag 的文件里，桌面端与 Android TV 端共用同一类型，
// 避免两端各自定义导致 group 名不一致。
type ServiceOut struct {
	fx.Out

	Service application.Service `group:"wails-services"`
}
