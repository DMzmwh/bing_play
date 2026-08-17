package service

import (
	"changeme/internal/logger"
	"changeme/internal/torrentmgr"
	"context"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// TorrentService
//
// Wails 对外暴露的 Torrent 业务服务。
// 负责：
//  1. 接收前端调用
//  2. 调用 TorrentManager
//  3. 返回业务结果
//
// 不负责：
//  1. 下载任务调度
//  2. 并发控制
//  3. torrent 底层生命周期
//  4. piece 下载逻辑
type TorrentService struct {
	manager *torrentmgr.TorrentManager
}

// NewTorrentService 创建 TorrentService
func NewTorrentService(manager *torrentmgr.TorrentManager) *TorrentService {
	return &TorrentService{
		manager: manager,
	}
}

// ServiceStartup is called when the application starts
func (s *TorrentService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	logger.App.Info("TorrentService ServiceStartup")

	return nil
}

// ServiceShutdown is called when the application shuts down
func (s *TorrentService) ServiceShutdown() error {
	logger.App.Info("关闭项目")
	err := s.manager.Shutdown()
	if err != nil {
		return err
	}
	return nil
}

// AddMagnet 添加 Magnet
func (s *TorrentService) AddMagnet(magnet string) error {
	if magnet == "" {
		return fmt.Errorf("magnet is empty")
	}

	return s.manager.AddMagnet(magnet)
}

// AddTorrent 添加 torrent 文件
func (s *TorrentService) AddTorrent(path string) error {
	if path == "" {
		return fmt.Errorf("torrent path is empty")
	}

	return s.manager.AddTorrent(path)
}

// Pause 暂停下载
func (s *TorrentService) Pause(hashInfo string) error {
	if hashInfo == "" {
		return fmt.Errorf("torrent hash is empty")
	}

	return s.manager.PauseJob(hashInfo)
}

// Remove 删除任务
func (s *TorrentService) Remove(hashInfo string) error {
	if hashInfo == "" {
		return fmt.Errorf("torrent hash is empty")
	}

	return s.manager.CancelJob(hashInfo)
}

// Get 获取任务信息
func (s *TorrentService) Get(hashInfo string) (*torrentmgr.TorrentJob, error) {
	if hashInfo == "" {
		return nil, fmt.Errorf("torrent hash is empty")
	}

	return nil, nil
}

// List 获取所有任务
func (s *TorrentService) List() []*torrentmgr.TorrentJob {
	return nil
}
