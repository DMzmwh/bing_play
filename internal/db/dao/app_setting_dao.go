package dao

import (
	"errors"

	"github.com/bing/bing-play/internal/db/models"
	"gorm.io/gorm"
)

type AppSettingDAO struct {
	db *gorm.DB
}

func NewAppSettingDAO(db *gorm.DB) *AppSettingDAO {
	return &AppSettingDAO{db: db}
}

func (d *AppSettingDAO) Get(key string) (string, error) {
	var setting models.AppSetting
	err := d.db.First(&setting, "key = ?", key).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return setting.Value, nil
}

func (d *AppSettingDAO) Set(key, value string) error {
	setting := models.AppSetting{
		Key:   key,
		Value: value,
	}
	return d.db.Save(&setting).Error
}

// GetAll 读取全部设置项，返回 key -> value 映射。
func (d *AppSettingDAO) GetAll() (map[string]string, error) {
	var rows []models.AppSetting
	if err := d.db.Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make(map[string]string, len(rows))
	for _, row := range rows {
		result[row.Key] = row.Value
	}
	return result, nil
}

// SetMany 在一个事务中批量写入设置项，任意一项失败时整体回滚。
func (d *AppSettingDAO) SetMany(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}

	return d.db.Transaction(func(tx *gorm.DB) error {
		for key, value := range values {
			setting := models.AppSetting{Key: key, Value: value}
			if err := tx.Save(&setting).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
