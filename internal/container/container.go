package container

import (
	"changeme/internal/db/dao"
	"changeme/internal/i18n"

	"gorm.io/gorm"
)

type Container struct {
	DB          *gorm.DB
	I18n        *i18n.I18nService
	ResourceDao *dao.ResourceDAO
}

func NewContainer(db *gorm.DB, i18n *i18n.I18nService) *Container {
	resourceDao := dao.NewResourceDAO(db)

	return &Container{
		DB:          db,
		I18n:        i18n,
		ResourceDao: resourceDao,
	}

}
