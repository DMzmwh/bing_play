package dhtcrawler

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/DMzmwh/bing-dhtcrawler/crawler"
	"github.com/DMzmwh/bing-dhtcrawler/crawler/metrics"
	"github.com/DMzmwh/bing-dhtcrawler/dht"
	"github.com/DMzmwh/bing-dhtcrawler/metadata"
	anacrolix "github.com/anacrolix/dht/v2"
	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/logger"
	"github.com/bing/bing-play/internal/settings"
	boom "github.com/tylertreat/BoomFilters"
	"go.uber.org/fx"
)

const (
	// 拉取种子元数据的超时。
	metadataTimeout = 10 * time.Second
	// InfoHash 去重 Bloom Filter 的容量与误判率。
	bloomCapacity  = 1_000_000
	bloomFalseRate = 0.01
)

// NodeRecord 是 FileStorage 写入 nodes.json 的 DHT 节点快照。
type NodeRecord struct {
	ID              string    `json:"id"`
	Addr            string    `json:"addr"`
	LastRespondedAt time.Time `json:"last_responded_at"`
}

type Service struct {
	cfg         *config.Config
	resourceDAO *dao.ResourceDAO
	settingDAO  *dao.AppSettingDAO
	cancel      context.CancelFunc
	mu          sync.Mutex
}

// NewService 注册到 Fx 生命周期，应用启动即自动拉起爬虫。
// 是否启用与监听端口优先取 app_settings 中的设置，没有设置时使用启动配置。
func NewService(lc fx.Lifecycle, cfg *config.Config, resourceDAO *dao.ResourceDAO, settingDAO *dao.AppSettingDAO) *Service {
	s := &Service{cfg: cfg, resourceDAO: resourceDAO, settingDAO: settingDAO}

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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startCrawler()
}

// ReloadCrawler 设置变更后立即按最新配置重启爬虫，无需等待应用重启。
func (s *Service) ReloadCrawler() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopCrawler()
	s.startCrawler()
}

func (s *Service) startCrawler() {
	if s.cfg == nil {
		logger.App.Info("DHT 爬虫未启用，跳过启动")
		return
	}
	if logger.App == nil {
		return
	}

	enabled, port := s.runtimeOptions()
	if !enabled {
		logger.App.Info("DHT 爬虫未启用，跳过启动")
		return
	}

	store := NewResourceStorage(s.resourceDAO)

	client, err := newDHTClient(port)
	if err != nil {
		logger.Error.Error("DHT 爬虫启动失败：DHT 客户端创建失败",
			slog.String("err", err.Error()),
			slog.Int("udp_port", port),
		)
		return
	}

	m, err := metrics.InitMetrics()
	if err != nil {
		client.Close()
		logger.Error.Error("DHT 爬虫启动失败：metrics 初始化失败", slog.String("err", err.Error()))
		return
	}

	// Fx 的启动上下文带超时，爬虫需要独立的长生命周期上下文。
	ctx, cancel := context.WithCancel(context.Background())

	c := crawler.NewBingCrawler(
		client,
		m,
		crawler.NewBingData(),
		metadata.NewRequester(dht.RandomNodeID(), metadataTimeout),
		store,
		crawler.NewIgnoreHashes(boom.NewDefaultStableBloomFilter(bloomCapacity, bloomFalseRate)),
		logger.Torrent.With(slog.String("component", "dhtcrawler")),
	)
	c.Start(ctx)

	s.cancel = cancel

	logger.App.Info("DHT 爬虫已启动",
		slog.String("storage", "resources 表"),
		slog.Int("udp_port", port),
	)

	go func() {
		<-ctx.Done()
		client.Close()
	}()
}

// runtimeOptions 优先读取 app_settings 中的爬虫分组配置，读取失败时使用启动配置。
func (s *Service) runtimeOptions() (bool, int) {
	enabled := s.cfg.CrawlerEnabled
	port := s.cfg.CrawlerPort

	if raw, err := s.settingDAO.Get(settings.KeyCrawler); err == nil && raw != "" {
		if parsed, ok := settings.ParseCrawlerSettings(raw); ok {
			if parsed.Enabled != nil {
				enabled = *parsed.Enabled
			}
			if parsed.Port != nil && *parsed.Port >= 1024 && *parsed.Port <= 65535 {
				port = *parsed.Port
			}
		}
	} else {
		// 兼容旧版本分散存储的扁平键
		if value, err := s.settingDAO.Get(settings.KeyCrawlerEnabled); err == nil && value != "" {
			if parsed, err := strconv.ParseBool(value); err == nil {
				enabled = parsed
			}
		}
		if value, err := s.settingDAO.Get(settings.KeyCrawlerPort); err == nil && value != "" {
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1024 && parsed <= 65535 {
				port = parsed
			}
		}
	}

	return enabled, port
}

func (s *Service) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopCrawler()
}

func (s *Service) stopCrawler() {
	if s.cancel == nil {
		return
	}
	s.cancel()
	s.cancel = nil
	logger.App.Info("DHT 爬虫已停止")
}

func newDHTClient(port int) (dht.Client, error) {
	cfg := anacrolix.NewDefaultServerConfig()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	})
	if err != nil {
		return nil, fmt.Errorf("监听 UDP %d 失败: %w", port, err)
	}

	cfg.Conn = conn

	client, err := dht.NewAnacrolixClient(cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("创建 DHT 客户端失败: %w", err)
	}

	return client, nil
}
