package dhtcrawler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/bing/bing-dhtcrawler/dht"
	"github.com/bing/bing-dhtcrawler/storage"
)

// 爬虫数据在 resources 下的相对目录。
const crawlerDataDir = "crawler"

// FileStorage 实现 bing-dhtcrawler 的 storage.Storage，
// 把种子元数据、DHT 来源统计追加写入 resources/crawler 下的 JSON Lines 文件。
type FileStorage struct {
	dir string
	mu  sync.Mutex
}

func NewFileStorage(resourceDir string) (*FileStorage, error) {
	abs, err := filepath.Abs(resolveResourceDir(resourceDir))
	if err != nil {
		return nil, fmt.Errorf("解析资源目录失败: %w", err)
	}

	dir := filepath.Join(abs, crawlerDataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录 %s 失败: %w", dir, err)
	}

	// 先做一次写探测，避免爬虫跑起来后才发现目录不可写。
	probe := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return nil, fmt.Errorf("数据目录不可写 %s: %w", dir, err)
	}
	_ = os.Remove(probe)

	return &FileStorage{dir: dir}, nil
}

// Dir 返回数据目录的绝对路径。
func (s *FileStorage) Dir() string {
	return s.dir
}

func (s *FileStorage) NodesPath() string {
	return filepath.Join(s.dir, "nodes.json")
}

func (s *FileStorage) SaveTorrents(ctx context.Context, items []any) error {
	return s.appendJSONLines(ctx, "torrents.jsonl", items)
}

func (s *FileStorage) SaveSources(ctx context.Context, items []any) error {
	return s.appendJSONLines(ctx, "sources.jsonl", items)
}

// GetTriageInfo 没有接入 triage 策略，返回空结果即可。
func (s *FileStorage) GetTriageInfo(ctx context.Context, hashes []dht.InfoHash) (map[dht.InfoHash]storage.TriageInfo, error) {
	return map[dht.InfoHash]storage.TriageInfo{}, nil
}

func (s *FileStorage) appendJSONLines(ctx context.Context, name string, items []any) error {
	if len(items) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开 %s 失败: %w", path, err)
	}

	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)

	for _, item := range items {
		select {
		case <-ctx.Done():
			f.Close()
			return ctx.Err()
		default:
		}

		if err := enc.Encode(item); err != nil {
			f.Close()
			return fmt.Errorf("写入 %s 失败: %w", path, err)
		}
	}

	if err := w.Flush(); err != nil {
		f.Close()
		return fmt.Errorf("刷新 %s 失败: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("关闭 %s 失败: %w", path, err)
	}

	return nil
}

// WriteNodes 原子写入 DHT 路由表节点快照（整体覆盖）。
func (s *FileStorage) WriteNodes(records []NodeRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化节点快照失败: %w", err)
	}

	path := s.NodesPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("替换 %s 失败: %w", path, err)
	}

	return nil
}

// resolveResourceDir 返回可用的资源目录：
// 配置目录存在就用它，否则退回可执行文件同级目录，最后再退回相对路径。
func resolveResourceDir(configured string) string {
	if configured == "" {
		configured = "resources"
	}

	if abs, err := filepath.Abs(configured); err == nil {
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			return abs
		}
	}

	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), filepath.Base(configured))
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}

	return configured
}
