package vo

import "github.com/bing/bing-play/internal/db/models"

// TorrentJobVO 下载任务快照。
// 用于事件推送（download:progress / download:state-changed）与下载页列表展示。
type TorrentJobVO struct {
	// InfoHash Torrent 唯一标识，前端按该字段更新对应任务。
	InfoHash string `json:"infoHash"`
	// Name 资源名称，来自 Torrent 元信息。
	Name string `json:"name"`
	// FileName 主视频文件名，未识别时为空。
	FileName string `json:"fileName"`
	// Status 当前下载状态。
	Status models.JobStatus `json:"status"`
	// Progress 下载进度，范围 0-100。
	Progress float64 `json:"progress"`
	// Downloaded 已下载字节数。
	Downloaded int64 `json:"downloaded"`
	// TotalSize 资源总字节数，元信息未获取时为 0。
	TotalSize int64 `json:"totalSize"`
	// DownloadSpeed 瞬时下载速度，单位 B/s。
	DownloadSpeed int64 `json:"downloadSpeed"`
	// UploadSpeed 瞬时上传速度，单位 B/s。
	UploadSpeed int64 `json:"uploadSpeed"`
	// Peers 当前活跃 Peer 数量。
	Peers int `json:"peers"`
	// EtaSeconds 预计剩余时间（秒），-1 表示无法估算。
	EtaSeconds int64 `json:"etaSeconds"`
	// Priority 下载优先级，数值越大越先下载。
	Priority int `json:"priority"`
	// RetryCount 已自动重试次数。
	RetryCount int `json:"retryCount"`
	// ErrorMessage 最近一次失败原因（i18n key），成功时为空。
	ErrorMessage string `json:"errorMessage"`
	// SavePath 任务创建时确定的下载目录。
	SavePath string `json:"savePath"`
	// CreatedTime 任务创建时间，格式 2006-01-02 15:04:05。
	CreatedTime string `json:"createdTime"`
	// CompletedTime 完成时间，未完成时为空。
	CompletedTime string `json:"completedTime"`
}
