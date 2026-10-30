package dhtcrawler

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/DMzmwh/bing-dhtcrawler/crawler"
	"github.com/DMzmwh/bing-dhtcrawler/crawler/metrics"
	"github.com/DMzmwh/bing-dhtcrawler/dht"
	"github.com/DMzmwh/bing-dhtcrawler/metadata"
	anacrolix "github.com/anacrolix/dht/v2"
	"github.com/bing/bing-play/internal/config"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/logger"
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
	cancel      context.CancelFunc
}

// NewService 注册到 Fx 生命周期，应用启动即自动拉起爬虫。
func NewService(lc fx.Lifecycle, cfg *config.Config, resourceDAO *dao.ResourceDAO) *Service {
	s := &Service{cfg: cfg, resourceDAO: resourceDAO}

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
	if s.cfg == nil || !s.cfg.CrawlerEnabled {
		logger.App.Info("DHT 爬虫未启用，跳过启动")
		return
	}
	if logger.App == nil {
		return
	}

	store := NewResourceStorage(s.resourceDAO)

	client, err := newDHTClient(s.cfg.CrawlerPort)
	if err != nil {
		logger.Error.Error("DHT 爬虫启动失败：DHT 客户端创建失败",
			slog.String("err", err.Error()),
			slog.Int("udp_port", s.cfg.CrawlerPort),
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
		slog.Int("udp_port", s.cfg.CrawlerPort),
	)

	go func() {
		<-ctx.Done()
		client.Close()
	}()
}

func (s *Service) stop() {
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
