package dhtcrawler

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DMzmwh/bing-dhtcrawler/dht"
	"github.com/DMzmwh/bing-dhtcrawler/storage"
)

func TestFileStorageWritesUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()

	s, err := NewFileStorage(dataDir)
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}

	expectedDir := filepath.Join(dataDir, crawlerDataDir)
	if s.Dir() != expectedDir {
		t.Fatalf("数据目录 = %s, 期望 %s", s.Dir(), expectedDir)
	}

	ctx := context.Background()

	torrents := []any{
		storage.TorrentRecord{
			InfoHash:    "0123456789abcdef0123456789abcdef01234567",
			Name:        "示例视频",
			Size:        1024,
			PieceLength: 256,
			FileCount:   1,
			Files: []storage.TorrentFileRecord{
				{Path: "示例视频.mp4", Size: 1024},
			},
			NodeAddr: "1.2.3.4:6881",
		},
	}
	if err := s.SaveTorrents(ctx, torrents); err != nil {
		t.Fatalf("SaveTorrents: %v", err)
	}

	sources := []any{
		storage.SourceRecord{InfoHash: "0123456789abcdef0123456789abcdef01234567", Seeders: 3, Leechers: 5},
	}
	if err := s.SaveSources(ctx, sources); err != nil {
		t.Fatalf("SaveSources: %v", err)
	}

	if err := s.WriteNodes([]NodeRecord{{ID: "aa", Addr: "5.6.7.8:6881"}}); err != nil {
		t.Fatalf("WriteNodes: %v", err)
	}

	lines := readJSONLines(t, filepath.Join(expectedDir, "torrents.jsonl"))
	if len(lines) != 1 || lines[0]["name"] != "示例视频" {
		t.Fatalf("torrents.jsonl 内容异常: %v", lines)
	}

	lines = readJSONLines(t, filepath.Join(expectedDir, "sources.jsonl"))
	if len(lines) != 1 || lines[0]["seeders"] != float64(3) {
		t.Fatalf("sources.jsonl 内容异常: %v", lines)
	}

	raw, err := os.ReadFile(filepath.Join(expectedDir, "nodes.json"))
	if err != nil {
		t.Fatalf("读取 nodes.json: %v", err)
	}
	var nodes []NodeRecord
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatalf("解析 nodes.json: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Addr != "5.6.7.8:6881" {
		t.Fatalf("nodes.json 内容异常: %+v", nodes)
	}
}

func TestFileStorageUnwritableDataDirIsReported(t *testing.T) {
	base := t.TempDir()
	blocked := filepath.Join(base, "data")

	// 用同名文件占位，让 MkdirAll 必然失败，验证错误会被返回而不是静默忽略。
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatalf("准备文件: %v", err)
	}

	if _, err := NewFileStorage(base); err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

func TestGetTriageInfoIsEmpty(t *testing.T) {
	s, err := NewFileStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}

	info, err := s.GetTriageInfo(context.Background(), []dht.InfoHash{{1, 2, 3}})
	if err != nil {
		t.Fatalf("GetTriageInfo: %v", err)
	}
	if len(info) != 0 {
		t.Fatalf("期望空 map，实际 %d 项", len(info))
	}
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开 %s: %v", path, err)
	}
	defer f.Close()

	var out []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var item map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatalf("解析 %s: %v", path, err)
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}

	return out
}
