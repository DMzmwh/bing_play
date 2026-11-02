package dao

import (
	"time"

	"github.com/bing/bing-play/internal/db/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TvDAO 电视直播订阅源与频道数据访问。
type TvDAO struct {
	db *gorm.DB
}

func NewTvDAO(db *gorm.DB) *TvDAO {
	return &TvDAO{
		db: db,
	}
}

// ListSources 查询全部订阅源，按 ID 升序。
func (d *TvDAO) ListSources() ([]models.TVSource, error) {
	var list []models.TVSource
	err := d.db.Order("id ASC").Find(&list).Error
	return list, err
}

// ListEnabledSources 查询启用的订阅源，供定时同步使用。
func (d *TvDAO) ListEnabledSources() ([]models.TVSource, error) {
	var list []models.TVSource
	err := d.db.Where("enabled <> 0").Order("id ASC").Find(&list).Error
	return list, err
}

// ListChannels 查询全部频道，按分组与原始顺序排序。
func (d *TvDAO) ListChannels() ([]models.TVChannel, error) {
	var list []models.TVChannel
	err := d.db.
		Order("group_title ASC, sort_order ASC, id ASC").
		Find(&list).
		Error
	return list, err
}

type tvSourceCount struct {
	SourceID int64 `gorm:"column:source_id"`
	Count    int64 `gorm:"column:count"`
}

// CountChannelsBySource 统计每个订阅源的频道数。
func (d *TvDAO) CountChannelsBySource() (map[int64]int64, error) {
	var rows []tvSourceCount
	err := d.db.
		Model(&models.TVChannel{}).
		Select("source_id, COUNT(*) AS count").
		Group("source_id").
		Scan(&rows).
		Error
	if err != nil {
		return nil, err
	}

	result := make(map[int64]int64, len(rows))
	for _, row := range rows {
		result[row.SourceID] = row.Count
	}
	return result, nil
}

// UpdateSourceSync 写回订阅源同步状态。
func (d *TvDAO) UpdateSourceSync(
	id int64,
	syncedAt time.Time,
	lastError string,
	etag string,
	lastModified string,
) error {
	now := time.Now()
	return d.db.Model(&models.TVSource{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_sync_at":  &syncedAt,
			"last_error":    lastError,
			"etag":          etag,
			"last_modified": lastModified,
			"update_time":   &now,
		}).
		Error
}

// ReplaceChannels 用本次解析结果替换指定订阅源的频道。
//
// 按 channel_key upsert 保留频道主键，避免前端列表在每次同步后整体换新；
// 同步完成后删除该订阅源中本次未出现的旧频道。
func (d *TvDAO) ReplaceChannels(sourceID int64, channels []models.TVChannel) error {
	if sourceID == 0 {
		return nil
	}

	return d.db.Transaction(func(tx *gorm.DB) error {
		if len(channels) > 0 {
			err := tx.
				Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "channel_key"}},
					DoUpdates: clause.AssignmentColumns([]string{
						"source_id",
						"tvg_id",
						"tvg_name",
						"name",
						"logo",
						"group_title",
						"stream_url",
						"duration",
						"sort_order",
						"update_time",
					}),
				}).
				CreateInBatches(channels, 200).
				Error
			if err != nil {
				return err
			}
		}

		var existing []models.TVChannel
		err := tx.
			Select("id, channel_key").
			Where("source_id = ?", sourceID).
			Find(&existing).
			Error
		if err != nil {
			return err
		}

		keep := make(map[string]struct{}, len(channels))
		for i := range channels {
			keep[channels[i].ChannelKey] = struct{}{}
		}

		stale := make([]int64, 0, len(existing))
		for i := range existing {
			if _, ok := keep[existing[i].ChannelKey]; !ok {
				stale = append(stale, existing[i].ID)
			}
		}

		// 分批删除，避免单条 SQL 参数过多。
		const batchSize = 500
		for start := 0; start < len(stale); start += batchSize {
			end := start + batchSize
			if end > len(stale) {
				end = len(stale)
			}
			if err := tx.
				Where("id IN ?", stale[start:end]).
				Delete(&models.TVChannel{}).
				Error; err != nil {
				return err
			}
		}

		return nil
	})
}
