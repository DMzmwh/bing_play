package vo

// TorrentJobVO 下载任务快照。
// 用于事件推送（download:progress / download:state-changed）与下载页列表展示。
//
// 状态字段均为字符串形式，前端直接拼装语言包 key，无需感知后端枚举数值：
//   - TaskState 下载任务生命周期
//   - LocalState 本地文件可用性
//   - DisplayState 由 (Kind, TaskState, LocalState) 派生的展示态
type TorrentJobVO struct {
	// InfoHash Torrent 唯一标识，前端按该字段更新对应任务。
	InfoHash string `json:"infoHash"`
	// Name 资源名称，来自 Torrent 元信息。
	Name string `json:"name"`
	// FileName 主视频文件名，未识别时为空。
	FileName string `json:"fileName"`
	// TaskState 下载任务生命周期状态。
	TaskState string `json:"taskState"`
	// LocalState 本地文件可用性状态。
	LocalState string `json:"localState"`
	// DisplayState 前端展示态。
	DisplayState string `json:"displayState"`
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
	// Attempt 当前连续失败次数。
	Attempt int `json:"attempt"`
	// MaxAttempts 自动重试上限。
	MaxAttempts int `json:"maxAttempts"`
	// ErrorCode 最近一次失败原因（i18n key），成功时为空。
	ErrorCode string `json:"errorCode"`
	// NextRetryAt 下一次自动重试时间，非重试等待状态时为空。
	NextRetryAt string `json:"nextRetryAt"`
	// SavePath 任务创建时确定的下载目录。
	SavePath string `json:"savePath"`
	// CreatedTime 任务创建时间，格式 2006-01-02 15:04:05。
	CreatedTime string `json:"createdTime"`
	// VerifiedAt 最近一次校验通过时间，未通过时为空。
	VerifiedAt string `json:"verifiedAt"`
}
