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
