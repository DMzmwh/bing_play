package models

import "time"

type AppSetting struct {
	Key       string    `gorm:"primaryKey;size:100" json:"key"`
	Value     string    `gorm:"type:text;not null" json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (AppSetting) TableName() string {
	return "app_settings"
}
