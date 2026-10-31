package dao

import (
	"errors"
	"strings"

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

// downloadTaskStatuses 下载页需要展示的任务状态，原始记录（0）与本地视频不在其中。
var downloadTaskStatuses = []models.JobStatus{
	models.JobStatusDownloadMetaInfo,
	models.JobStatusQueued,
	models.JobStatusDownloading,
	models.JobStatusPaused,
	models.JobStatusCompleted,
	models.JobStatusFailed,
	models.JobStatusCancelled,
}

// ListDownloadTasks 查询全部下载任务，按更新时间倒序。
func (d *ResourceDAO) ListDownloadTasks() ([]models.Resource, error) {
	var list []models.Resource
	err := d.db.
		Where("download_status IN ?", downloadTaskStatuses).
		Order("update_time DESC").
		Find(&list).
		Error
	return list, err
}

// ListByDownloadStatuses 按下载状态查询资源，供启动时恢复未完成任务。
func (d *ResourceDAO) ListByDownloadStatuses(statuses []models.JobStatus) ([]models.Resource, error) {
	if len(statuses) == 0 {
		return nil, nil
	}

	var list []models.Resource
	err := d.db.Where("download_status IN ?", statuses).Find(&list).Error
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
func (d *ResourceDAO) ListPlayable() ([]models.Resource, error) {
	var list []models.Resource

	err := d.db.
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
