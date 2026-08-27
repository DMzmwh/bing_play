package models

import (
	"changeme/internal/filetype"
	"time"
)

// Resource 资源实体。
// 用于保存资源、Torrent、本地文件、下载任务以及播放相关信息。
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

	// ResourceType 文件  类型，例如video
	ResourceType filetype.Category `gorm:"column:resource_type"`

	// =========================================================
	// 本地文件状态
	// =========================================================

	// LocalStatus 本地文件状态。
	//
	// not_downloaded：尚未下载
	// exists：文件存在且正常
	// missing：数据库记录存在，但本地文件不存在
	// corrupted：本地文件存在，但文件损坏
	// deleted：用户主动删除了本地文件
	LocalStatus LocalStatus `gorm:"column:local_status;type:text"`

	// =========================================================
	// 下载状态
	// =========================================================

	// DownloadStatus 下载任务状态。
	//
	// fetching_meta_info：正在获取 Torrent 元信息
	// queued：等待下载
	// downloading：下载中
	// paused：已暂停
	// completed：下载完成
	// failed：下载失败
	// cancelled：已取消
	DownloadStatus JobStatus `gorm:"column:download_status;type:text"`

	// =========================================================
	// 下载控制
	// =========================================================

	// Priority 下载优先级。
	// 数值越大，优先级越高。
	Priority *int `gorm:"default:0"`

	// RetryCount 当前任务已经自动重试的次数。
	RetryCount *int `gorm:"default:0"`

	// MateRetryCount 最大自动重试次数。
	MateRetryCount *int `gorm:"default:0"`

	// ErrorMessage 最近一次下载失败的错误信息。
	ErrorMessage *string `gorm:"column:error_message"`

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

	// CompletedTime 下载完成时间。
	// 未完成时为 nil。
	CompletedTime *time.Time `gorm:"column:completed_time"`
}
