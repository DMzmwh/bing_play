package dhtcrawler

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/bing/bing-dhtcrawler/dht"
	"github.com/bing/bing-dhtcrawler/storage"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
)

// ResourceStorage 实现 bing-dhtcrawler 的 storage.Storage，
// 把抓取到的种子元数据写入 resources 表。
// FileStorage 的 JSON Lines 落盘实现保留，可在需要时切回。
type ResourceStorage struct {
	resourceDAO *dao.ResourceDAO
}

func NewResourceStorage(resourceDAO *dao.ResourceDAO) *ResourceStorage {
	return &ResourceStorage{resourceDAO: resourceDAO}
}

// SaveTorrents 把种子元数据批量写入 resources 表。
func (s *ResourceStorage) SaveTorrents(ctx context.Context, items []any) error {
	if len(items) == 0 {
		return nil
	}

	list := make([]*models.Resource, 0, len(items))
	for _, item := range items {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		record, ok := asTorrentRecord(item)
		if !ok {
			continue
		}
		list = append(list, toResource(record))
	}

	if len(list) == 0 {
		return nil
	}

	if err := s.resourceDAO.SaveCrawledResources(list); err != nil {
		return fmt.Errorf("写入 resources 表失败: %w", err)
	}

	return nil
}

// SaveSources resources 表没有 seeder/leecher 字段，来源统计暂不落库。
func (s *ResourceStorage) SaveSources(ctx context.Context, items []any) error {
	return nil
}

// GetTriageInfo 没有接入 triage 策略，返回空结果即可。
func (s *ResourceStorage) GetTriageInfo(ctx context.Context, hashes []dht.InfoHash) (map[dht.InfoHash]storage.TriageInfo, error) {
	return map[dht.InfoHash]storage.TriageInfo{}, nil
}

// asTorrentRecord 兼容值类型与指针类型的记录。
func asTorrentRecord(item any) (storage.TorrentRecord, bool) {
	switch v := item.(type) {
	case storage.TorrentRecord:
		return v, true
	case *storage.TorrentRecord:
		if v != nil {
			return *v, true
		}
	}

	return storage.TorrentRecord{}, false
}

// toResource 把爬虫记录转换成 resources 表实体。
// 爬虫只负责发现资源，因此本地状态固定为未下载。
func toResource(record storage.TorrentRecord) *models.Resource {
	name := record.Name
	size := record.Size
	sourceURL := buildSourceURL(record.InfoHash, record.PeerAddr)
	discoveredAt := record.DiscoveredAt
	if discoveredAt.IsZero() {
		discoveredAt = time.Now()
	}

	return &models.Resource{
		ResourceName:   &name,
		InfoHash:       record.InfoHash,
		SourceURL:      &sourceURL,
		TotalSize:      &size,
		LocalStatus:    models.LocalStatusNotDownloaded,
		DownloadStatus: models.JobStatus0,
		CreateTime:     &discoveredAt,
		UpdateTime:     &discoveredAt,
	}
}

// buildSourceURL 生成磁力链接。
// 抓取时提供元数据的节点已确认持有该种子，作为 x.pe 参数写入，
// 下载端可直接连上它拉取元数据与分片，不必再等 DHT 重新发现 peer。
func buildSourceURL(infoHash, peerAddr string) string {
	magnet := "magnet:?xt=urn:btih:" + infoHash
	if peerAddr == "" {
		return magnet
	}

	return magnet + "&x.pe=" + url.QueryEscape(peerAddr)
}
