package models

import (
	"time"

	"github.com/bing/bing-play/internal/filetype"
)

// Resource 资源实体。
// 用于保存资源、Torrent、本地文件、下载任务以及播放相关信息。
//
// 状态采用双轴模型：
//   - TaskState 下载任务生命周期
//   - LocalState 本地文件可用性
//   - LocalLossReason 本地文件丢失 / 损坏的成因
//
// 两轴组合由 ValidPair 校验，前端展示态由 DisplayOf 派生，不落库。
type Resource struct {

	// =========================================================
	// 主键
	// =========================================================

	// ID 资源主键，自增 ID。
	ID int64 `gorm:"primaryKey;autoIncrement"`

	// =========================================================
	// 资源信息
	// =========================================================

	// ResourceName 资源名称，例如电影名称、视频名称。
	ResourceName *string `gorm:"column:resource_name"`

	// FileName 文件名称，例如 movie.mkv。
	FileName *string `gorm:"column:file_name"`

	// SourceURL 资源来源地址，例如 Magnet、HTTP 下载地址等。
	SourceURL *string `gorm:"column:source_url"`

	// CoverURL 资源封面图片地址。
	CoverURL *string `gorm:"column:cover_url"`

	// =========================================================
	// Torrent 信息
	// =========================================================

	// InfoHash Torrent InfoHash，用于唯一标识一个 Torrent。
	InfoHash string `gorm:"column:info_hash"`

	// TorrentPath 本地 .torrent 文件路径。
	TorrentPath *string `gorm:"column:torrent_path"`

	// TotalSize Torrent 总大小，单位：Byte。
	TotalSize *int64 `gorm:"default:0"`

	// =========================================================
	// 本地文件
	// =========================================================

	// LocalPath 下载文件在本地的绝对路径。
	LocalPath string `gorm:"column:local_path"`

	// FileSize 本地文件实际大小，单位：Byte。
	FileSize *int64 `gorm:"default:0"`

	// DownloadedSize 已经下载的数据量，单位：Byte。
	DownloadedSize *int64 `gorm:"default:0"`

	// MimeType 文件 MIME 类型，例如 video/mp4、video/x-matroska。
	MimeType *string `gorm:"column:mime_type"`

	// ResourceType 文件类型，例如 video。
	ResourceType filetype.Category `gorm:"column:resource_type"`

	// =========================================================
	// 状态机
	// =========================================================

	// Kind 资源来源类型，决定是否参与下载状态机与是否进入下载列表。
	Kind Kind `gorm:"column:kind;default:0"`

	// TaskState 下载任务生命周期状态。
	TaskState TaskState `gorm:"column:task_state;default:0"`

	// LocalState 本地文件可用性状态。
	LocalState LocalState `gorm:"column:local_state;default:0"`

	// LocalLossReason 本地文件缺失 / 损坏的成因。
	LocalLossReason LocalLossReason `gorm:"column:local_loss_reason;default:0"`

	// StateVersion 每次成功流转 +1，用于并发 CAS 落库。
	StateVersion uint64 `gorm:"column:state_version;default:0"`

	// Attempt 当前连续失败次数。
	Attempt uint16 `gorm:"column:attempt;default:0"`

	// MaxAttempts 创建时快照的自动重试上限。
	MaxAttempts uint16 `gorm:"column:max_attempts;default:3"`

	// ErrorCode 最近一次失败原因的 i18n key，成功时为空。
	ErrorCode string `gorm:"column:error_code;default:''"`

	// NextRetryAt 下一次自动重试时间点，非 RetryWait 状态时为 nil。
	NextRetryAt *time.Time `gorm:"column:next_retry_at"`

	// StateChangedAt 最后一次状态变更时间。
	StateChangedAt *time.Time `gorm:"column:state_changed_at"`

	// VerifiedAt 最近一次校验通过时间。
	VerifiedAt *time.Time `gorm:"column:verified_at"`

	// LocalCheckedAt 最近一次本地文件检查时间。
	LocalCheckedAt *time.Time `gorm:"column:local_checked_at"`

	// =========================================================
	// 下载控制
	// =========================================================

	// Priority 下载优先级。
	// 数值越大，优先级越高。
	Priority *int `gorm:"default:0"`

	// =========================================================
	// 播放信息
	// =========================================================

	// TotalDuration 媒体总时长，单位：秒。
	TotalDuration *int64 `gorm:"default:0"`

	// Played 是否曾经播放过。
	//
	// false：未播放
	// true：已播放
	Played *bool `gorm:"default:0"`

	// PlayProgress 当前播放进度，单位：秒。
	PlayProgress *int64 `gorm:"default:0"`

	// PlayCount 播放次数。
	PlayCount *int `gorm:"default:0"`

	// LastPlayTime 最后一次播放时间。
	LastPlayTime *time.Time `gorm:"column:last_play_time"`

	// Finished 是否已经播放完成。
	//
	// false：未播放完成
	// true：已经播放完成
	Finished *bool `gorm:"default:0"`

	// =========================================================
	// 收藏 / 标签
	// =========================================================

	// Favorite 是否收藏。
	//
	// false：未收藏
	// true：已收藏
	Favorite *bool `gorm:"default:0"`

	// Tags 资源标签。
	// 可以保存 JSON，例如 ["电影", "动作"]。
	Tags *string `gorm:"column:tags"`

	// Remark 用户备注信息。
	Remark *string `gorm:"column:remark"`

	// =========================================================
	// 时间
	// =========================================================

	// CreateTime 资源创建时间。
	CreateTime *time.Time `gorm:"column:create_time;default:CURRENT_TIMESTAMP"`

	// UpdateTime 资源最后更新时间。
	UpdateTime *time.Time `gorm:"column:update_time;default:CURRENT_TIMESTAMP"`
}

// DisplayState 返回前端展示态，O(1)。
func (r *Resource) Display() DisplayState {
	if r == nil {
		return ShowFailed
	}
	return DisplayOf(r.Kind, r.TaskState, r.LocalState)
}
