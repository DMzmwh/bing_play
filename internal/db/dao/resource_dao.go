package dao

import (
	"errors"
	"strings"
	"time"

	"github.com/bing/bing-play/internal/db/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResourceDAO struct {
	db *gorm.DB
}

func NewResourceDAO(db *gorm.DB) *ResourceDAO {
	return &ResourceDAO{
		db: db,
	}
}

// WithTx 支持事务
func (d *ResourceDAO) WithTx(tx *gorm.DB) *ResourceDAO {
	return &ResourceDAO{
		db: tx,
	}
}

// Save 新增资源
func (d *ResourceDAO) Save(r *models.Resource) error {
	return d.db.Save(r).Error
}

// SaveLocalResource 保存本地视频资源。
// 本地拖入的视频没有 Torrent InfoHash，保持该列为 NULL，
// 避免触发 resources 表上 info_hash 的唯一索引冲突。
func (d *ResourceDAO) SaveLocalResource(r *models.Resource) error {
	return d.db.Omit("info_hash").Save(r).Error
}

// UpdateState 以 CAS 方式写回状态与进度快照。
//
// fromVersion 为本次写入前数据库中期望的 state_version。
// 返回 false 表示版本已变化（存在更新的写入），本次陈旧快照被丢弃。
func (d *ResourceDAO) UpdateState(r *models.Resource, fromVersion uint64) (bool, error) {
	if r == nil || r.ID == 0 {
		return false, nil
	}

	result := d.db.Model(&models.Resource{}).
		Where("id = ? AND state_version = ?", r.ID, fromVersion).
		Updates(map[string]any{
			"task_state":        int8(r.TaskState),
			"local_state":       int8(r.LocalState),
			"local_loss_reason": int8(r.LocalLossReason),
			"state_version":     r.StateVersion,
			"attempt":           r.Attempt,
			"max_attempts":      r.MaxAttempts,
			"error_code":        r.ErrorCode,
			"next_retry_at":     r.NextRetryAt,
			"state_changed_at":  r.StateChangedAt,
			"verified_at":       r.VerifiedAt,
			"local_checked_at":  r.LocalCheckedAt,
			"downloaded_size":   r.DownloadedSize,
			"file_size":         r.FileSize,
			"total_size":        r.TotalSize,
			"resource_name":     r.ResourceName,
			"file_name":         r.FileName,
			"local_path":        r.LocalPath,
			"resource_type":     r.ResourceType,
			"update_time":       r.UpdateTime,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// UpdatePriority 仅更新优先级，不改变状态版本。
func (d *ResourceDAO) UpdatePriority(id int64, priority int) error {
	if id == 0 {
		return nil
	}
	now := time.Now()
	return d.db.Model(&models.Resource{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"priority":    priority,
			"update_time": &now,
		}).
		Error
}

// GetByInfoHash 根据 InfoHash 查询资源，不存在时返回 nil。
func (d *ResourceDAO) GetByInfoHash(infoHash string) (*models.Resource, error) {
	if infoHash == "" {
		return nil, nil
	}

	var r models.Resource
	err := d.db.Where("info_hash = ?", infoHash).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetByIDs 按主键批量查询资源。
func (d *ResourceDAO) GetByIDs(ids []int64) ([]models.Resource, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var list []models.Resource
	err := d.db.Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// downloadTaskStates 下载页需要展示的任务状态，Discovered（仅爬虫发现）与本地视频不在其中。
var downloadTaskStates = []models.TaskState{
	models.TaskPreparing,
	models.TaskQueued,
	models.TaskDownloading,
	models.TaskPaused,
	models.TaskVerifying,
	models.TaskRetryWait,
	models.TaskCompleted,
	models.TaskFailed,
	models.TaskCancelled,
}

// int8Slice 把强类型状态枚举转成驱动可直接绑定的切片。
func int8Slice[T ~int8](values []T) []int8 {
	out := make([]int8, len(values))
	for i, v := range values {
		out[i] = int8(v)
	}
	return out
}

// ListDownloadTasks 查询全部种子下载任务（排除本地视频与纯爬虫记录），按更新时间倒序。
func (d *ResourceDAO) ListDownloadTasks() ([]models.Resource, error) {
	var list []models.Resource
	err := d.db.
		Where("kind = ? AND task_state IN ?", int8(models.KindTorrent), int8Slice(downloadTaskStates)).
		Order("update_time DESC").
		Find(&list).
		Error
	return list, err
}

// ListCompletedTasks 查询已完成的种子下载任务，用于下载页"已完成"标签页。
func (d *ResourceDAO) ListCompletedTasks() ([]models.Resource, error) {
	var list []models.Resource
	err := d.db.
		Where("kind = ? AND task_state = ?", int8(models.KindTorrent), int8(models.TaskCompleted)).
		Order("verified_at DESC").
		Find(&list).
		Error
	return list, err
}

// ListByTaskStates 按任务状态查询资源，供启动时恢复未完成任务。
func (d *ResourceDAO) ListByTaskStates(states []models.TaskState) ([]models.Resource, error) {
	if len(states) == 0 {
		return nil, nil
	}

	var list []models.Resource
	err := d.db.
		Where("kind = ? AND task_state IN ?", int8(models.KindTorrent), int8Slice(states)).
		Find(&list).
		Error
	return list, err
}

// Get 根据ID查询
func (d *ResourceDAO) Get(id string) (*models.Resource, error) {

	//## 4、优先级顺序（重要）
	//1. `.Table("xxx")` → **最高，硬指定表，无视 model**
	//2. model 的 `TableName()` 方法返回值
	//3. GORM 默认规则：结构体名转小写复数
	//> 你原代码：没有 `.Table()`，没有 `.Model()`，直接 `First(&r)`
	//> GORM 拿传入的 `&r`（*models.Resource），调用它的 TableName ()；没实现就走默认复数规则。

	var r models.Resource

	err := d.db.
		Where("id = ?", id).
		First(&r).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	return &r, err
}

// ListPlayable 查询播放列表所需的本地资源，按资源名称、创建时间排序。
//
// 只返回"本地文件齐全"的记录：
//   - 本地视频：KindLocal + LocalPresent
//   - 种子资源：KindTorrent + TaskCompleted + LocalPresent
func (d *ResourceDAO) ListPlayable() ([]models.Resource, error) {
	var list []models.Resource

	err := d.db.
		Where(
			"local_state = ? AND (kind = ? OR task_state = ?)",
			int8(models.LocalPresent),
			int8(models.KindLocal),
			int8(models.TaskCompleted),
		).
		Where("local_path <> ''").
		Order("resource_name ASC, create_time ASC").
		Find(&list).
		Error

	return list, err
}

// SaveCrawledResources 批量写入爬虫抓取到的种子资源。
// 依靠 info_hash 上的唯一索引做去重：已存在的记录直接跳过，避免重复插入。
func (d *ResourceDAO) SaveCrawledResources(list []*models.Resource) error {
	if len(list) == 0 {
		return nil
	}

	return d.db.
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(list, 100).
		Error
}

// CrawledListQuery 已爬取资源列表的查询条件。
type CrawledListQuery struct {
	// Keyword 按资源名称或 InfoHash 模糊匹配。
	Keyword string
	// SortBy 排序字段：create_time / total_size / resource_name。
	SortBy string
	// SortOrder 排序方向：asc / desc。
	SortOrder string
	// Offset 跳过条数。
	Offset int
	// Limit 返回条数。
	Limit int
}

// crawledSortColumns 允许的排序字段白名单，避免拼接到 SQL 时被注入。
var crawledSortColumns = map[string]string{
	"create_time":   "create_time",
	"total_size":    "total_size",
	"resource_name": "resource_name",
}

// ListCrawled 分页查询爬虫抓取到的资源。
// 只返回带 InfoHash 的记录，本地拖入的视频文件不属于爬取数据。
func (d *ResourceDAO) ListCrawled(q CrawledListQuery) ([]models.Resource, int64, error) {
	base := d.db.Model(&models.Resource{}).Where("info_hash IS NOT NULL AND info_hash <> ''")

	if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		base = base.Where("resource_name LIKE ? OR info_hash LIKE ?", like, like)
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	column, ok := crawledSortColumns[q.SortBy]
	if !ok {
		column = "create_time"
	}
	desc := q.SortOrder != "asc"

	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	var list []models.Resource
	err := base.Session(&gorm.Session{}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: column}, Desc: desc}).
		Offset(q.Offset).
		Limit(limit).
		Find(&list).
		Error
	if err != nil {
		return nil, 0, err
	}

	return list, total, nil
}

// GetByLocalPath 根据本地文件路径查询资源
func (d *ResourceDAO) GetByLocalPath(path string) (*models.Resource, error) {

	var r models.Resource

	err := d.db.
		Where("local_path = ?", path).
		First(&r).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	return &r, err
}

//
//// func GetPeerStats(
////
////	datasouce *gorm.DB,
////
//// ) ([]PeerStat, error) {
//func (d *ResourceDAO) GetPeerStats() ([]models.PeerStat, error) {
//
//	var result []models.PeerStat
//
//	err := d.db.Raw(`
//		SELECT peer_id,COUNT(*) as count FROM resources
//		GROUP BY peer_id
//		ORDER BY count DESC
//	`).Scan(&result).Error
//
//	return result, err
//}
