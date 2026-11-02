package models

import (
	"time"

	"github.com/bing/bing-play/internal/filetype"
)

// ResourceRecord resources 表：爬虫发现 / 下载来源的资源元数据。
// 以 InfoHash 为唯一标识，不保存任何下载状态。
type ResourceRecord struct {
	InfoHash     string            `gorm:"column:info_hash;primaryKey"`
	ResourceName *string           `gorm:"column:resource_name"`
	SourceURL    *string           `gorm:"column:source_url"`
	CoverURL     *string           `gorm:"column:cover_url"`
	TotalSize    *int64            `gorm:"default:0"`
	TorrentPath  *string           `gorm:"column:torrent_path"`
	ResourceType filetype.Category `gorm:"column:resource_type"`
	Favorite     *bool             `gorm:"default:0"`
	Tags         *string           `gorm:"column:tags"`
	Remark       *string           `gorm:"column:remark"`
	CreateTime   *time.Time        `gorm:"column:create_time;default:CURRENT_TIMESTAMP"`
	UpdateTime   *time.Time        `gorm:"column:update_time;default:CURRENT_TIMESTAMP"`
}

// TableName 指定 resources 表。
func (ResourceRecord) TableName() string { return "resources" }

// DownloadTaskRecord download_tasks 表：下载任务生命周期。
// 以 InfoHash 为主键，name / source_url / torrent_path / total_size 为展示快照，
// 下载列表不依赖 resources 表。
type DownloadTaskRecord struct {
	InfoHash       string     `gorm:"column:info_hash;primaryKey"`
	Name           *string    `gorm:"column:name"`
	SourceURL      *string    `gorm:"column:source_url"`
	TorrentPath    *string    `gorm:"column:torrent_path"`
	TotalSize      *int64     `gorm:"default:0"`
	TaskState      TaskState  `gorm:"column:task_state;default:0"`
	StateVersion   uint64     `gorm:"column:state_version;default:0"`
	Attempt        uint16     `gorm:"column:attempt;default:0"`
	MaxAttempts    uint16     `gorm:"column:max_attempts;default:3"`
	ErrorCode      string     `gorm:"column:error_code;default:''"`
	NextRetryAt    *time.Time `gorm:"column:next_retry_at"`
	StateChangedAt *time.Time `gorm:"column:state_changed_at"`
	Priority       *int       `gorm:"default:0"`
	DownloadedSize *int64     `gorm:"default:0"`
	SavePath       string     `gorm:"column:save_path;default:''"`
	UpdateTime     *time.Time `gorm:"column:update_time;default:CURRENT_TIMESTAMP"`
}

// TableName 指定 download_tasks 表。
func (DownloadTaskRecord) TableName() string { return "download_tasks" }

// LocalFileRecord local_files 表：本地文件 / 播放项。
// 保留自增 ID 作为播放身份；info_hash 为 NULL 表示本地拖入的视频。
type LocalFileRecord struct {
	ID              int64           `gorm:"column:id;primaryKey;autoIncrement"`
	InfoHash        *string         `gorm:"column:info_hash"`
	ResourceName    *string         `gorm:"column:resource_name"`
	LocalState      LocalState      `gorm:"column:local_state;default:0"`
	LocalLossReason LocalLossReason `gorm:"column:local_loss_reason;default:0"`
	LocalPath       string          `gorm:"column:local_path;default:''"`
	FileName        *string         `gorm:"column:file_name"`
	FileSize        *int64          `gorm:"default:0"`
	MimeType        *string         `gorm:"column:mime_type"`
	VerifiedAt      *time.Time      `gorm:"column:verified_at"`
	LocalCheckedAt  *time.Time      `gorm:"column:local_checked_at"`
	TotalDuration   *int64          `gorm:"default:0"`
	Played          *bool           `gorm:"default:0"`
	PlayProgress    *int64          `gorm:"default:0"`
	PlayCount       *int            `gorm:"default:0"`
	LastPlayTime    *time.Time      `gorm:"column:last_play_time"`
	Finished        *bool           `gorm:"default:0"`
	CreateTime      *time.Time      `gorm:"column:create_time;default:CURRENT_TIMESTAMP"`
	UpdateTime      *time.Time      `gorm:"column:update_time;default:CURRENT_TIMESTAMP"`
}

// TableName 指定 local_files 表。
func (LocalFileRecord) TableName() string { return "local_files" }

// Split 把聚合实体拆成三张表的行。
// 本地视频（InfoHash 为空）不产生 download_tasks 行。
func Split(r *Resource) (*ResourceRecord, *DownloadTaskRecord, *LocalFileRecord) {
	if r == nil {
		return nil, nil, nil
	}

	resource := &ResourceRecord{
		InfoHash:     r.InfoHash,
		ResourceName: r.ResourceName,
		SourceURL:    r.SourceURL,
		CoverURL:     r.CoverURL,
		TotalSize:    r.TotalSize,
		TorrentPath:  r.TorrentPath,
		ResourceType: r.ResourceType,
		Favorite:     r.Favorite,
		Tags:         r.Tags,
		Remark:       r.Remark,
		CreateTime:   r.CreateTime,
		UpdateTime:   r.UpdateTime,
	}

	var task *DownloadTaskRecord
	if r.InfoHash != "" {
		task = &DownloadTaskRecord{
			InfoHash:       r.InfoHash,
			Name:           r.ResourceName,
			SourceURL:      r.SourceURL,
			TorrentPath:    r.TorrentPath,
			TotalSize:      r.TotalSize,
			TaskState:      r.TaskState,
			StateVersion:   r.StateVersion,
			Attempt:        r.Attempt,
			MaxAttempts:    r.MaxAttempts,
			ErrorCode:      r.ErrorCode,
			NextRetryAt:    r.NextRetryAt,
			StateChangedAt: r.StateChangedAt,
			Priority:       r.Priority,
			DownloadedSize: r.DownloadedSize,
			SavePath:       r.LocalPath,
			UpdateTime:     r.UpdateTime,
		}
	}

	local := &LocalFileRecord{
		ID:              r.ID,
		ResourceName:    r.ResourceName,
		LocalState:      r.LocalState,
		LocalLossReason: r.LocalLossReason,
		LocalPath:       r.LocalPath,
		FileName:        r.FileName,
		FileSize:        r.FileSize,
		MimeType:        r.MimeType,
		VerifiedAt:      r.VerifiedAt,
		LocalCheckedAt:  r.LocalCheckedAt,
		TotalDuration:   r.TotalDuration,
		Played:          r.Played,
		PlayProgress:    r.PlayProgress,
		PlayCount:       r.PlayCount,
		LastPlayTime:    r.LastPlayTime,
		Finished:        r.Finished,
		CreateTime:      r.CreateTime,
		UpdateTime:      r.UpdateTime,
	}
	if r.InfoHash != "" {
		hash := r.InfoHash
		local.InfoHash = &hash
	}

	return resource, task, local
}

// Assemble 把三张表的行装配回聚合实体。
// 缺失的 download_tasks 行等价于 Discovered，缺失的 local_files 行等价于 LocalNone。
func Assemble(infoHash string, resource *ResourceRecord, task *DownloadTaskRecord, local *LocalFileRecord) *Resource {
	r := &Resource{InfoHash: infoHash}
	r.Kind = KindTorrent
	if infoHash == "" {
		r.Kind = KindLocal
	}

	if resource != nil {
		r.ResourceName = resource.ResourceName
		r.SourceURL = resource.SourceURL
		r.CoverURL = resource.CoverURL
		r.TotalSize = resource.TotalSize
		r.TorrentPath = resource.TorrentPath
		r.ResourceType = resource.ResourceType
		r.Favorite = resource.Favorite
		r.Tags = resource.Tags
		r.Remark = resource.Remark
		r.CreateTime = resource.CreateTime
		r.UpdateTime = resource.UpdateTime
	}

	if task != nil {
		r.TaskState = task.TaskState
		r.StateVersion = task.StateVersion
		r.Attempt = task.Attempt
		r.MaxAttempts = task.MaxAttempts
		r.ErrorCode = task.ErrorCode
		r.NextRetryAt = task.NextRetryAt
		r.StateChangedAt = task.StateChangedAt
		r.Priority = task.Priority
		r.DownloadedSize = task.DownloadedSize
		if r.ResourceName == nil {
			r.ResourceName = task.Name
		}
		if r.SourceURL == nil {
			r.SourceURL = task.SourceURL
		}
		if r.TotalSize == nil {
			r.TotalSize = task.TotalSize
		}
		if r.TorrentPath == nil {
			r.TorrentPath = task.TorrentPath
		}
		if r.LocalPath == "" {
			r.LocalPath = task.SavePath
		}
		if r.CreateTime == nil {
			r.CreateTime = task.UpdateTime
		}
	} else {
		r.MaxAttempts = 3
		if r.Kind == KindLocal {
			r.TaskState = TaskCompleted
		}
	}

	if local != nil {
		r.ID = local.ID
		r.LocalState = local.LocalState
		r.LocalLossReason = local.LocalLossReason
		r.LocalPath = local.LocalPath
		r.FileName = local.FileName
		r.FileSize = local.FileSize
		r.MimeType = local.MimeType
		r.VerifiedAt = local.VerifiedAt
		r.LocalCheckedAt = local.LocalCheckedAt
		r.TotalDuration = local.TotalDuration
		r.Played = local.Played
		r.PlayProgress = local.PlayProgress
		r.PlayCount = local.PlayCount
		r.LastPlayTime = local.LastPlayTime
		r.Finished = local.Finished
		if r.ResourceName == nil {
			r.ResourceName = local.ResourceName
		}
		if r.CreateTime == nil {
			r.CreateTime = local.CreateTime
		}
		if local.UpdateTime != nil {
			r.UpdateTime = local.UpdateTime
		}
	} else {
		r.LocalState = LocalNone
	}

	return r
}
