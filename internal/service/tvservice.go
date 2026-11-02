package service

import (
	"github.com/bing/bing-play/internal/tv"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// TVService 电视直播服务，负责把频道数据与同步操作暴露给前端。
type TVService struct {
	tv *tv.Service
}

// NewTVService 创建电视直播服务。
func NewTVService(s *tv.Service) *TVService {
	return &TVService{
		tv: s,
	}
}

// NewTVWailsService 把电视直播服务注册为 Wails service。
func NewTVWailsService(s *TVService) ServiceOut {
	return ServiceOut{
		Service: application.NewService(s),
	}
}

// GetTvSnapshot 一次返回频道列表与订阅源状态，减少 IPC 往返。
func (s *TVService) GetTvSnapshot() (*tv.Snapshot, error) {
	return s.tv.Snapshot()
}
