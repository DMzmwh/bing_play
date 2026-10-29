package dao

import (
	"errors"
	"github.com/bing/bing-play/internal/db/models"

	"gorm.io/gorm"
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
