package tv

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/logger"

	"github.com/wailsapp/wails/v3/pkg/application"
	"go.uber.org/fx"
)

const (
	// RefreshInterval 订阅源自动更新间隔。
	RefreshInterval = 6 * time.Hour
	// requestTimeout 单次订阅源请求超时。
	requestTimeout = 30 * time.Second
	// userAgent 请求订阅源时使用的 UA。
	userAgent = "bing-play/1.0 (+https://github.com/bing/bing-play)"
	// maxBodySize 订阅文件大小上限，避免异常响应占用过多内存。
	maxBodySize = 32 << 20
	// EventChannelsUpdated 频道数据更新后广播的事件名。
	EventChannelsUpdated = "tv:channels-updated"
)

// 同步失败时写入 tv_sources.last_error 的错误码，前端按 i18n key 展示。
const (
	ErrorCodeFetchFailed = "tv.error.fetch_failed"
	ErrorCodeStatus      = "tv.error.status"
	ErrorCodeParse       = "tv.error.parse"
	ErrorCodeEmpty       = "tv.error.empty"
	ErrorCodeStore       = "tv.error.internal"
)

// ChannelItem 返回给前端的频道信息。
type ChannelItem struct {
	ID         int64  `json:"id"`
	SourceID   int64  `json:"sourceId"`
	TvgID      string `json:"tvgId"`
	TvgName    string `json:"tvgName"`
	Name       string `json:"name"`
	Logo       string `json:"logo"`
	GroupTitle string `json:"groupTitle"`
	StreamURL  string `json:"streamUrl"`
}

// SourceStatus 订阅源同步状态。
type SourceStatus struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	Enabled      bool   `json:"enabled"`
	LastSyncAt   string `json:"lastSyncAt"`
	LastError    string `json:"lastError"`
	ChannelCount int64  `json:"channelCount"`
}

// Snapshot 前端首屏需要的完整数据：频道列表 + 订阅源状态。
type Snapshot struct {
	Channels []ChannelItem  `json:"channels"`
	Sources  []SourceStatus `json:"sources"`
}

// Service 电视直播订阅服务。
//
// 启动时只同步距上次请求已达 RefreshInterval 的订阅源，之后按各源上次同步时间
// 计算下一次请求；拉取结果按频道写入 tv_channels，并广播 EventChannelsUpdated。
type Service struct {
	dao    *dao.TvDAO
	client *http.Client

	// syncMu 串行化同步任务，避免启动同步与定时任务并发写库。
	syncMu sync.Mutex

	// cacheMu 保护内存快照。频道数据只在同步写库后整体替换，
	// 前端读取时直接复用，避免每次请求都全量查库并逐行映射。
	cacheMu    sync.RWMutex
	cacheReady bool
	channels   []ChannelItem
	statuses   []SourceStatus

	cancel context.CancelFunc
	done   chan struct{}
}

// NewService 创建电视直播服务并注册到 Fx 生命周期。
func NewService(lc fx.Lifecycle, tvDAO *dao.TvDAO) *Service {
	s := &Service{
		dao: tvDAO,
		client: &http.Client{
			Timeout: requestTimeout,
		},
	}

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			s.start()
			return nil
		},
		OnStop: func(context.Context) error {
			s.stop()
			return nil
		},
	})

	return s
}

func (s *Service) start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	done := make(chan struct{})
	s.done = done

	go func() {
		defer close(done)

		// 先用库里已有数据热缓存，保证页面在首次同步完成前也能正常打开。
		s.refreshSnapshot()

		for {
			// 只同步距上次请求已达 RefreshInterval 的订阅源；
			// 重启后未满 6 小时的源不会重新发起请求。
			_, _ = s.SyncAll(ctx)
			if ctx.Err() != nil {
				return
			}

			// 按各源上次同步时间计算下一次请求时间，严格保证 6 小时间隔。
			timer := time.NewTimer(s.nextSyncDelay())
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (s *Service) stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.done != nil {
		<-s.done
	}
}

// Snapshot 返回频道列表与订阅源状态的完整快照，供前端一次调用取齐。
func (s *Service) Snapshot() (*Snapshot, error) {
	if err := s.ensureSnapshot(); err != nil {
		return nil, err
	}

	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return &Snapshot{
		Channels: s.channels,
		Sources:  s.statuses,
	}, nil
}

// Status 返回全部订阅源的同步状态。
func (s *Service) Status() ([]SourceStatus, error) {
	if err := s.ensureSnapshot(); err != nil {
		return nil, err
	}

	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return s.statuses, nil
}

// ensureSnapshot 保证内存快照可用，首次访问时从数据库加载。
func (s *Service) ensureSnapshot() error {
	s.cacheMu.RLock()
	ready := s.cacheReady
	s.cacheMu.RUnlock()
	if ready {
		return nil
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cacheReady {
		return nil
	}
	return s.rebuildSnapshotLocked()
}

// refreshSnapshot 同步写库后重建快照，保证前端收到事件时能立即读到新数据。
func (s *Service) refreshSnapshot() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if err := s.rebuildSnapshotLocked(); err != nil {
		// 重建失败时保留旧快照，避免前端列表突然变空。
		logError("电视频道快照刷新失败", slog.Any("err", err))
	}
}

// rebuildSnapshotLocked 从数据库读取并重建快照，调用方需持有 cacheMu 写锁。
func (s *Service) rebuildSnapshotLocked() error {
	rows, err := s.dao.ListChannels()
	if err != nil {
		return err
	}

	items := make([]ChannelItem, 0, len(rows))
	for i := range rows {
		items = append(items, ChannelItem{
			ID:         rows[i].ID,
			SourceID:   rows[i].SourceID,
			TvgID:      rows[i].TvgID,
			TvgName:    rows[i].TvgName,
			Name:       rows[i].Name,
			Logo:       rows[i].Logo,
			GroupTitle: rows[i].GroupTitle,
			StreamURL:  rows[i].StreamURL,
		})
	}

	sources, err := s.dao.ListSources()
	if err != nil {
		return err
	}
	counts, err := s.dao.CountChannelsBySource()
	if err != nil {
		return err
	}

	statuses := make([]SourceStatus, 0, len(sources))
	for i := range sources {
		status := SourceStatus{
			ID:           sources[i].ID,
			Name:         sources[i].Name,
			URL:          sources[i].URL,
			Enabled:      sources[i].Enabled,
			LastError:    sources[i].LastError,
			ChannelCount: counts[sources[i].ID],
		}
		if sources[i].LastSyncAt != nil {
			status.LastSyncAt = sources[i].LastSyncAt.Format("2006-01-02 15:04:05")
		}
		statuses = append(statuses, status)
	}

	s.channels = items
	s.statuses = statuses
	s.cacheReady = true
	return nil
}

// SyncAll 立即同步全部启用的订阅源，返回同步后的状态。
//
// 单个订阅源失败不会中断其它订阅源，错误记录在对应 SourceStatus.LastError 中。
// 距上次同步不足 RefreshInterval 的订阅源会被跳过，重启后也不会提前请求。
func (s *Service) SyncAll(ctx context.Context) ([]SourceStatus, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	sources, err := s.dao.ListEnabledSources()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	for i := range sources {
		if ctx.Err() != nil {
			break
		}
		if !isSyncDue(&sources[i], now) {
			continue
		}
		s.syncSource(ctx, &sources[i])
	}

	return s.Status()
}

// isSyncDue 判断订阅源距上次同步是否已达更新间隔。
func isSyncDue(source *models.TVSource, now time.Time) bool {
	if source.LastSyncAt == nil {
		return true
	}
	return now.Sub(*source.LastSyncAt) >= RefreshInterval
}

// minSyncDelay 异常情况下（如状态写回失败）的最小重试间隔，避免密集请求。
const minSyncDelay = time.Minute

// nextSyncDelay 返回距离最近一个订阅源到期还需等待的时间。
func (s *Service) nextSyncDelay() time.Duration {
	sources, err := s.dao.ListEnabledSources()
	if err != nil {
		logError("电视订阅源读取失败", slog.Any("err", err))
		return RefreshInterval
	}
	if len(sources) == 0 {
		return RefreshInterval
	}

	now := time.Now()
	delay := RefreshInterval
	for i := range sources {
		// 从未同步过的源应按最短间隔重试。
		if sources[i].LastSyncAt == nil {
			return minSyncDelay
		}
		remaining := RefreshInterval - now.Sub(*sources[i].LastSyncAt)
		if remaining < delay {
			delay = remaining
		}
	}

	if delay < minSyncDelay {
		return minSyncDelay
	}
	return delay
}

// syncSource 拉取并落库单个订阅源。
func (s *Service) syncSource(ctx context.Context, source *models.TVSource) {
	now := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		s.finishSync(source.ID, now, ErrorCodeFetchFailed, source.ETag, source.LastModified)
		return
	}
	req.Header.Set("User-Agent", userAgent)
	if source.ETag != "" {
		req.Header.Set("If-None-Match", source.ETag)
	}
	if source.LastModified != "" {
		req.Header.Set("If-Modified-Since", source.LastModified)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			logError("电视订阅源请求失败", slog.Int64("source_id", source.ID), slog.Any("err", err))
		}
		s.finishSync(source.ID, now, ErrorCodeFetchFailed, source.ETag, source.LastModified)
		return
	}
	defer resp.Body.Close()

	etag := resp.Header.Get("ETag")
	lastModified := resp.Header.Get("Last-Modified")
	if etag == "" {
		etag = source.ETag
	}
	if lastModified == "" {
		lastModified = source.LastModified
	}

	switch {
	case resp.StatusCode == http.StatusNotModified:
		// 内容未变化，只刷新同步时间。
		s.finishSync(source.ID, time.Now(), "", etag, lastModified)
		return
	case resp.StatusCode != http.StatusOK:
		logError("电视订阅源返回异常状态",
			slog.Int64("source_id", source.ID),
			slog.Int("status", resp.StatusCode),
		)
		s.finishSync(source.ID, now, ErrorCodeStatus, etag, lastModified)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		logError("电视订阅源读取失败", slog.Int64("source_id", source.ID), slog.Any("err", err))
		s.finishSync(source.ID, now, ErrorCodeFetchFailed, etag, lastModified)
		return
	}

	parsed, err := ParseM3U(bytes.NewReader(body))
	if err != nil {
		logError("电视订阅源解析失败", slog.Int64("source_id", source.ID), slog.Any("err", err))
		s.finishSync(source.ID, now, ErrorCodeParse, etag, lastModified)
		return
	}
	if len(parsed) == 0 {
		s.finishSync(source.ID, now, ErrorCodeEmpty, etag, lastModified)
		return
	}

	channels := make([]models.TVChannel, 0, len(parsed))
	for i := range parsed {
		channels = append(channels, models.TVChannel{
			SourceID:   source.ID,
			ChannelKey: parsed[i].Key,
			TvgID:      parsed[i].TvgID,
			TvgName:    parsed[i].TvgName,
			Name:       parsed[i].Name,
			Logo:       parsed[i].Logo,
			GroupTitle: parsed[i].GroupTitle,
			StreamURL:  parsed[i].StreamURL,
			Duration:   parsed[i].Duration,
			SortOrder:  parsed[i].SortOrder,
			CreateTime: &now,
			UpdateTime: &now,
		})
	}

	if err := s.dao.ReplaceChannels(source.ID, channels); err != nil {
		logError("电视频道写入失败", slog.Int64("source_id", source.ID), slog.Any("err", err))
		s.finishSync(source.ID, now, ErrorCodeStore, etag, lastModified)
		return
	}

	s.finishSync(source.ID, now, "", etag, lastModified)
	emitChannelsUpdated(source.ID, len(channels))
}

// finishSync 写回订阅源同步状态。
func (s *Service) finishSync(
	sourceID int64,
	syncedAt time.Time,
	lastError string,
	etag string,
	lastModified string,
) {
	if err := s.dao.UpdateSourceSync(sourceID, syncedAt, lastError, etag, lastModified); err != nil {
		logError("电视订阅源状态写回失败", slog.Int64("source_id", sourceID), slog.Any("err", err))
	}

	// 同步状态与频道可能有变化，重建快照后再广播事件。
	s.refreshSnapshot()
}

// emitChannelsUpdated 广播频道更新事件，通知前端重新拉取列表。
func emitChannelsUpdated(sourceID int64, count int) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(EventChannelsUpdated, map[string]any{
		"sourceId": sourceID,
		"count":    count,
	})
}

func logError(message string, args ...any) {
	if logger.App == nil {
		return
	}
	logger.App.Error(message, args...)
}
