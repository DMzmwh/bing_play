package container

import (
	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/i18n"

	"gorm.io/gorm"
)

type Container struct {
	DB          *gorm.DB
	I18n        *i18n.I18nService
	ResourceDao *dao.ResourceDAO
	Cfg         *config.Config
}

func NewContainer(db *gorm.DB, cfg *config.Config, i18n *i18n.I18nService) *Container {
	resourceDao := dao.NewResourceDAO(db)

	return &Container{
		DB:          db,
		I18n:        i18n,
		Cfg:         cfg,
		ResourceDao: resourceDao,
	}

}
