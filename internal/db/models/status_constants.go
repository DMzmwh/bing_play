package models

// JobStatus 下载任务状态。
type JobStatus string

const (
	// 最原始状态
	JobStatus0 JobStatus = "0"
	// JobStatusDownloadMetaInfo 正在获取 Torrent 元信息。
	JobStatusDownloadMetaInfo JobStatus = "download_meta_info"
	// JobStatusQueued 已加入下载队列，等待下载。
	JobStatusQueued JobStatus = "queued"
	// JobStatusDownloading 正在下载。
	JobStatusDownloading JobStatus = "downloading"
	// JobStatusPaused 下载任务已暂停。
	JobStatusPaused JobStatus = "paused"
	// JobStatusCompleted 下载任务已完成。
	JobStatusCompleted JobStatus = "completed"
	// JobStatusFailed 下载任务执行失败。
	JobStatusFailed JobStatus = "failed"
)

// LocalStatus 本地文件状态。
type LocalStatus string

const (
	// LocalStatusNotDownloaded 尚未下载。
	LocalStatusNotDownloaded LocalStatus = "not_downloaded"
	// LocalStatusExists 本地文件存在且正常。
	LocalStatusExists LocalStatus = "exists"
	// LocalStatusMissing 数据库中存在资源记录，但本地文件不存在。
	LocalStatusMissing LocalStatus = "missing"
	// LocalStatusCorrupted 本地文件存在，但文件内容损坏或校验失败。
	LocalStatusCorrupted LocalStatus = "corrupted"
	// LocalStatusDeleted 本地文件已被用户删除。
	LocalStatusDeleted LocalStatus = "deleted"
)
