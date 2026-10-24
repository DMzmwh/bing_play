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
