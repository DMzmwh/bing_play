package service

import (
	"context"
	"errors"

	"github.com/bing/bing-play/internal/i18n"
	"github.com/bing/bing-play/internal/logger"
	"github.com/bing/bing-play/internal/torrentmgr"
	"github.com/bing/bing-play/internal/torrentmgr/models/vo"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// TorrentService
//
// Wails 对外暴露的 Torrent 业务服务。
// 负责：
//  1. 接收前端调用
//  2. 调用 TorrentManager
//  3. 把错误 key 翻译为当前语言提示
//
// 不负责：
//  1. 下载任务调度
//  2. 并发控制
//  3. torrent 底层生命周期
//  4. piece 下载逻辑
type TorrentService struct {
	manager *torrentmgr.TorrentManager
	i18n    *i18n.I18nService
}

// DownloadOptions 下载模块对外展示的运行参数（时间统一为秒，方便前端展示）。
type DownloadOptions struct {
	// MaxConcurrent 同时下载的任务数。
	MaxConcurrent int `json:"maxConcurrent"`
	// MaxRetries 失败后的最大自动重试次数。
	MaxRetries int `json:"maxRetries"`
	// MetadataTimeoutSeconds 获取 Torrent 元信息的最长等待秒数。
	MetadataTimeoutSeconds int `json:"metadataTimeoutSeconds"`
	// RetryDelaySeconds 自动重试前的等待秒数。
	RetryDelaySeconds int `json:"retryDelaySeconds"`
	// ProgressIntervalMillis 进度事件推送间隔（毫秒）。
	ProgressIntervalMillis int `json:"progressIntervalMillis"`
	// StallTimeoutSeconds 无下载进展判定为卡住的秒数。
	StallTimeoutSeconds int `json:"stallTimeoutSeconds"`
}

// NewTorrentService 创建 TorrentService
func NewTorrentService(manager *torrentmgr.TorrentManager, i18nService *i18n.I18nService) *TorrentService {
	return &TorrentService{
		manager: manager,
		i18n:    i18nService,
	}
}

func NewTorrentWailsService(s *TorrentService) ServiceOut {
	return ServiceOut{
		Service: application.NewService(s),
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
	return s.manager.Shutdown()
}

// AddMagnet 添加单个 Magnet 并立即入队。
func (s *TorrentService) AddMagnet(magnet string) error {
	return s.translateError(s.manager.AddMagnet(magnet))
}

// AddMagnets 批量添加磁力链接，返回新增 / 跳过 / 失败统计。
func (s *TorrentService) AddMagnets(magnets []string, priority int) *torrentmgr.BatchDownloadResult {
	result := s.manager.AddMagnets(magnets, priority)
	s.translateBatchErrors(result)
	return result
}

// AddTorrent 添加本地 torrent 文件并立即入队。
func (s *TorrentService) AddTorrent(path string) error {
	return s.translateError(s.manager.AddTorrent(path))
}

// DownloadResources 按资源 ID 批量加入下载队列，用于种子资源页的多选下载。
func (s *TorrentService) DownloadResources(ids []int64, priority int) (*torrentmgr.BatchDownloadResult, error) {
	result, err := s.manager.DownloadResources(ids, priority)
	if err != nil {
		return nil, s.translateError(err)
	}
	s.translateBatchErrors(result)
	return result, nil
}

// Pause 暂停下载任务，保留已完成分片用于继续。
func (s *TorrentService) Pause(infoHash string) error {
	return s.translateError(s.manager.PauseJob(infoHash))
}

// Resume 继续已暂停 / 失败 / 取消的任务。
func (s *TorrentService) Resume(infoHash string) error {
	return s.translateError(s.manager.ResumeJob(infoHash))
}

// Retry 重试失败任务，重置重试计数后重新拉取元信息。
func (s *TorrentService) Retry(infoHash string) error {
	return s.translateError(s.manager.RetryJob(infoHash))
}

// Repair 修复本地文件丢失的已完成任务，重新下载缺失文件。
func (s *TorrentService) Repair(infoHash string) error {
	return s.translateError(s.manager.RepairJob(infoHash))
}

// Cancel 取消下载任务，数据库记录保留用于列表展示。
func (s *TorrentService) Cancel(infoHash string) error {
	return s.translateError(s.manager.CancelJob(infoHash))
}

// SetPriority 调整任务优先级，数值越大越先下载。
func (s *TorrentService) SetPriority(infoHash string, priority int) error {
	return s.translateError(s.manager.SetPriority(infoHash, priority))
}

// Get 获取单个下载任务快照。
func (s *TorrentService) Get(infoHash string) (*vo.TorrentJobVO, error) {
	payload, err := s.manager.GetJobVO(infoHash)
	if err != nil {
		return nil, s.translateError(err)
	}
	return payload, nil
}

// List 获取全部下载任务快照，按状态与优先级排序。
func (s *TorrentService) List() []*vo.TorrentJobVO {
	return s.manager.ListJobs()
}

// ListActive 获取「待下载」列表：内存运行时任务用实时快照，其余未完成任务从数据库补齐。
func (s *TorrentService) ListActive() []*vo.TorrentJobVO {
	return s.manager.ListActiveJobs()
}

// ListCompleted 获取已完成任务，仅在打开下载页时调用一次。
func (s *TorrentService) ListCompleted() ([]*vo.TorrentJobVO, error) {
	rows, err := s.manager.ListCompletedJobs()
	if err != nil {
		return nil, s.translateError(err)
	}
	return rows, nil
}

// OpenDownloadDir 打开已完成任务的下载目录。
func (s *TorrentService) OpenDownloadDir(infoHash string) error {
	return s.translateError(s.manager.OpenDownloadDir(infoHash))
}

// GetOptions 返回下载模块当前运行参数，用于下载页展示并发与重试策略。
func (s *TorrentService) GetOptions() *DownloadOptions {
	cfg := s.manager.GetDownloadConfig()
	return &DownloadOptions{
		MaxConcurrent:          cfg.MaxConcurrent,
		MaxRetries:             cfg.MaxRetries,
		MetadataTimeoutSeconds: int(cfg.MetadataTimeout.Seconds()),
		RetryDelaySeconds:      int(cfg.RetryDelay.Seconds()),
		ProgressIntervalMillis: int(cfg.ProgressInterval.Milliseconds()),
		StallTimeoutSeconds:    int(cfg.StallTimeout.Seconds()),
	}
}

// translateBatchErrors 把批量结果中的错误 key 翻译为当前语言提示。
func (s *TorrentService) translateBatchErrors(result *torrentmgr.BatchDownloadResult) {
	if result == nil {
		return
	}
	for i, key := range result.Errors {
		result.Errors[i] = s.translateKey(key)
	}
}

// translateError 把错误中的语言包 key 翻译为当前语言提示，非语言包错误保持原样。
func (s *TorrentService) translateError(err error) error {
	if err == nil {
		return nil
	}
	key := torrentmgr.ErrorKey(err)
	if key == "" {
		return err
	}
	return errors.New(s.translateKey(key))
}

// translateKey 按当前语言翻译文案 key；翻译缺失时原样返回 key。
func (s *TorrentService) translateKey(key string) string {
	if key == "" || s.i18n == nil {
		return key
	}
	return s.i18n.T(key, nil)
}
