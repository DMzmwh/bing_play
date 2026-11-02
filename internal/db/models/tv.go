package models

import "time"

// TVSource 电视直播订阅源。
//
// 一个订阅源对应一份 M3U 文件，服务启动及定时任务会拉取并写入 tv_channels。
type TVSource struct {
	// ID 订阅源主键。
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`

	// Name 订阅源名称。
	Name string `gorm:"column:name;not null;default:''" json:"name"`

	// URL M3U 订阅地址，唯一。
	URL string `gorm:"column:url;not null" json:"url"`

	// Enabled 是否启用；停用后不参与定时同步。
	Enabled bool `gorm:"column:enabled" json:"enabled"`

	// LastSyncAt 最近一次同步时间，首次同步前为 nil。
	LastSyncAt *time.Time `gorm:"column:last_sync_at" json:"lastSyncAt"`

	// LastError 最近一次同步失败的错误码（i18n 消息 ID），成功时为空。
	LastError string `gorm:"column:last_error;not null;default:''" json:"lastError"`

	// ETag 服务端返回的 ETag，用于条件请求。
	ETag string `gorm:"column:etag;not null;default:''" json:"etag"`

	// LastModified 服务端返回的 Last-Modified，用于条件请求。
	LastModified string `gorm:"column:last_modified;not null;default:''" json:"lastModified"`

	// CreateTime 创建时间。
	CreateTime *time.Time `gorm:"column:create_time;default:CURRENT_TIMESTAMP" json:"createTime"`

	// UpdateTime 最后更新时间。
	UpdateTime *time.Time `gorm:"column:update_time;default:CURRENT_TIMESTAMP" json:"updateTime"`
}

func (TVSource) TableName() string {
	return "tv_sources"
}

// TVChannel 电视直播频道。
//
// 频道归属于订阅源，channel_key 唯一，用于同源重复同步时做 upsert。
type TVChannel struct {
	// ID 频道主键。
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`

	// SourceID 所属订阅源 ID。
	SourceID int64 `gorm:"column:source_id;not null" json:"sourceId"`

	// ChannelKey 频道唯一标识。
	ChannelKey string `gorm:"column:channel_key;not null" json:"channelKey"`

	// TvgID EPG 频道标识。
	TvgID string `gorm:"column:tvg_id;not null;default:''" json:"tvgId"`

	// TvgName 频道标准名称。
	TvgName string `gorm:"column:tvg_name;not null;default:''" json:"tvgName"`

	// Name 用户看到的频道显示名。
	Name string `gorm:"column:name;not null;default:''" json:"name"`

	// Logo 台标图片地址。
	Logo string `gorm:"column:logo;not null;default:''" json:"logo"`

	// GroupTitle 频道分组。
	GroupTitle string `gorm:"column:group_title;not null;default:''" json:"groupTitle"`

	// StreamURL 直播流地址。
	StreamURL string `gorm:"column:stream_url;not null" json:"streamUrl"`

	// Duration #EXTINF 中的节目时长，直播为 -1。
	Duration int `gorm:"column:duration;not null;default:-1" json:"duration"`

	// SortOrder 在订阅源中的原始顺序。
	SortOrder int `gorm:"column:sort_order;not null;default:0" json:"sortOrder"`

	// CreateTime 创建时间。
	CreateTime *time.Time `gorm:"column:create_time;default:CURRENT_TIMESTAMP" json:"createTime"`

	// UpdateTime 最后更新时间。
	UpdateTime *time.Time `gorm:"column:update_time;default:CURRENT_TIMESTAMP" json:"updateTime"`
}

func (TVChannel) TableName() string {
	return "tv_channels"
}
