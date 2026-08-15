package dao

import (
	"changeme/internal/db/models"
	"errors"

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

// Create 新增资源
func (d *ResourceDAO) Create(r *models.Resource) error {
	return d.db.Create(r).Error
}

// Get 根据ID查询
func (d *ResourceDAO) Get(id string) (*models.Resource, error) {

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

// func GetPeerStats(
//
//	datasouce *gorm.DB,
//
// ) ([]PeerStat, error) {
func (d *ResourceDAO) GetPeerStats() ([]models.PeerStat, error) {

	var result []models.PeerStat

	err := d.db.Raw(`
		SELECT peer_id,COUNT(*) as count FROM resources
		GROUP BY peer_id
		ORDER BY count DESC
	`).Scan(&result).Error

	return result, err
}
