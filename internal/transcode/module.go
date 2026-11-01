package transcode

import (
	"context"
	"path/filepath"

	"github.com/bing/bing-play/internal/config"
	"go.uber.org/fx"
)

var Module = fx.Module(
	"transcode",

	fx.Provide(NewSchedulerFromConfig),
)

// NewSchedulerFromConfig 从应用配置创建调度器，并在应用退出时释放资源。
//
// 缓存目录位于 DataDir/cache/transcode，原视频文件不会被修改。
func NewSchedulerFromConfig(lc fx.Lifecycle, cfg *config.Config) *Scheduler {
	scheduler := NewScheduler(Options{
		CacheDir: filepath.Join(cfg.DataDir, "cache", "transcode"),
		Policy:   DefaultPolicy(),
	})

	lc.Append(fx.Hook{
		OnStop: func(_ context.Context) error {
			scheduler.Close()
			return nil
		},
	})
	return scheduler
}
