package vo

import "changeme/internal/db/models"

type TorrentJobVO struct {
	Job      *models.Resource
	Status   models.JobStatus `json:"status"`
	Files    []*FileVo
	Progress float64 `json:"progress"`
	// 已下载大小
	Downloaded int64 `json:"downloaded"`
	// 总大小
	TotalSize int64 `json:"totalSize"`
	// 下载速度 B/s
	DownloadSpeed int64 `json:"downloadSpeed"`
	// 上传速度 B/s
	UploadSpeed int64 `json:"uploadSpeed"`
	// peer数量
	Peers int `json:"peers"`
	// 保存路径
	SavePath string `json:"savePath"`
}
