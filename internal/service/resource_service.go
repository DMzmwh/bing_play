package service

import (
	"time"

	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// CrawledResourceQuery 已爬取资源列表的查询参数。
// Page 从 1 开始，PageSize 会被限制在 [1, maxCrawledPageSize] 区间内。
type CrawledResourceQuery struct {
	Keyword   string `json:"keyword"`
	SortBy    string `json:"sortBy"`
	SortOrder string `json:"sortOrder"`
	Page      int    `json:"page"`
	PageSize  int    `json:"pageSize"`
}

// CrawledResourceItem 列表中的单条爬取资源。
type CrawledResourceItem struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	InfoHash       string `json:"infoHash"`
	SourceURL      string `json:"sourceUrl"`
	TotalSize      int64  `json:"totalSize"`
	LocalStatus    string `json:"localStatus"`
	DownloadStatus string `json:"downloadStatus"`
	// CreatedTime 爬取时间，格式固定为 2006-01-02 15:04:05，避免前端再做时区处理。
	CreatedTime string `json:"createdTime"`
}

// CrawledResourcePage 分页结果。
type CrawledResourcePage struct {
	Items    []CrawledResourceItem `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
}

// maxCrawledPageSize 单页最大条数，避免前端一次拉取过多数据。
const maxCrawledPageSize = 200

// ResourceService 资源库服务。
//
// 负责：
//  1. 分页查询爬虫抓取到的资源
//
// 不负责：
//  1. 下载调度，交给 TorrentService
//  2. 本地视频登记，交给 VideoService
type ResourceService struct {
	resourceDao *dao.ResourceDAO
}

// NewResourceService 创建 ResourceService
func NewResourceService(resourceDao *dao.ResourceDAO) *ResourceService {
	return &ResourceService{
		resourceDao: resourceDao,
	}
}

// NewResourceWailsService 把资源库服务暴露给前端。
func NewResourceWailsService(s *ResourceService) ServiceOut {
	return ServiceOut{
		Service: application.NewService(s),
	}
}

// ListCrawled 分页查询已爬取资源，支持关键词筛选与字段排序。
func (s *ResourceService) ListCrawled(query CrawledResourceQuery) (*CrawledResourcePage, error) {
	page := query.Page
	if page <= 0 {
		page = 1
	}

	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > maxCrawledPageSize {
		pageSize = maxCrawledPageSize
	}

	resources, total, err := s.resourceDao.ListCrawled(dao.CrawledListQuery{
		Keyword:   query.Keyword,
		SortBy:    query.SortBy,
		SortOrder: query.SortOrder,
		Offset:    (page - 1) * pageSize,
		Limit:     pageSize,
	})
	if err != nil {
		return nil, err
	}

	items := make([]CrawledResourceItem, 0, len(resources))
	for i := range resources {
		items = append(items, toCrawledResourceItem(&resources[i]))
	}

	return &CrawledResourcePage{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// toCrawledResourceItem 把数据库实体转换成前端展示用的条目。
func toCrawledResourceItem(resource *models.Resource) CrawledResourceItem {
	item := CrawledResourceItem{
		ID:             resource.ID,
		InfoHash:       resource.InfoHash,
		LocalStatus:    string(resource.LocalStatus),
		DownloadStatus: string(resource.DownloadStatus),
	}

	if resource.ResourceName != nil {
		item.Name = *resource.ResourceName
	}
	if resource.SourceURL != nil {
		item.SourceURL = *resource.SourceURL
	}
	if resource.TotalSize != nil {
		item.TotalSize = *resource.TotalSize
	}
	if resource.CreateTime != nil {
		item.CreatedTime = resource.CreateTime.Format(time.DateTime)
	}

	return item
}
