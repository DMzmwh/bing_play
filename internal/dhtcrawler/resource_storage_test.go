package dhtcrawler

import (
	"net/url"
	"testing"
	"time"

	"github.com/DMzmwh/bing-dhtcrawler/storage"
	"github.com/bing/bing-play/internal/db/models"

	"github.com/anacrolix/torrent"
)

func TestToResource(t *testing.T) {
	discovered := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	record := storage.TorrentRecord{
		InfoHash:     "0123456789abcdef0123456789abcdef01234567",
		Name:         "示例种子",
		Size:         1024,
		PeerAddrs:    []string{"1.2.3.4:6881", "5.6.7.8:51413"},
		DiscoveredAt: discovered,
	}

	got := toResource(record)

	if got.InfoHash != record.InfoHash {
		t.Fatalf("InfoHash = %q, want %q", got.InfoHash, record.InfoHash)
	}
	if got.ResourceName == nil || *got.ResourceName != record.Name {
		t.Fatalf("ResourceName = %v, want %q", got.ResourceName, record.Name)
	}
	if got.TotalSize == nil || *got.TotalSize != record.Size {
		t.Fatalf("TotalSize = %v, want %d", got.TotalSize, record.Size)
	}
	wantSource := "magnet:?xt=urn:btih:" + record.InfoHash +
		"&x.pe=" + url.QueryEscape(record.PeerAddrs[0]) +
		"&x.pe=" + url.QueryEscape(record.PeerAddrs[1])
	if got.SourceURL == nil || *got.SourceURL != wantSource {
		t.Fatalf("SourceURL = %v, want %q", got.SourceURL, wantSource)
	}
	if got.Kind != models.KindTorrent {
		t.Fatalf("Kind = %v, want %v", got.Kind, models.KindTorrent)
	}
	if got.TaskState != models.TaskDiscovered {
		t.Fatalf("TaskState = %v, want %v", got.TaskState, models.TaskDiscovered)
	}
	if got.LocalState != models.LocalNone {
		t.Fatalf("LocalState = %v, want %v", got.LocalState, models.LocalNone)
	}
	if got.CreateTime == nil || !got.CreateTime.Equal(discovered) {
		t.Fatalf("CreateTime = %v, want %v", got.CreateTime, discovered)
	}
}

func TestBuildSourceURL(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"

	if got := buildSourceURL(hash, nil); got != "magnet:?xt=urn:btih:"+hash {
		t.Fatalf("无 peer 时 SourceURL = %q", got)
	}
	if got := buildSourceURL(hash, []string{"", ""}); got != "magnet:?xt=urn:btih:"+hash {
		t.Fatalf("空 peer 地址应被忽略, SourceURL = %q", got)
	}

	peerAddrs := []string{"1.2.3.4:6881", "5.6.7.8:51413"}
	got := buildSourceURL(hash, peerAddrs)
	want := "magnet:?xt=urn:btih:" + hash +
		"&x.pe=" + url.QueryEscape(peerAddrs[0]) +
		"&x.pe=" + url.QueryEscape(peerAddrs[1])
	if got != want {
		t.Fatalf("SourceURL = %q, want %q", got, want)
	}

	// 生成的磁力链接必须能正确解析出 infohash 与全部 peer。
	spec, err := torrent.TorrentSpecFromMagnetUri(got)
	if err != nil {
		t.Fatalf("解析磁力链接失败: %v", err)
	}
	if spec.InfoHash.HexString() != hash {
		t.Fatalf("InfoHash = %q, want %q", spec.InfoHash.HexString(), hash)
	}
	if len(spec.PeerAddrs) != len(peerAddrs) {
		t.Fatalf("PeerAddrs = %v, want %v", spec.PeerAddrs, peerAddrs)
	}
	for i := range peerAddrs {
		if spec.PeerAddrs[i] != peerAddrs[i] {
			t.Fatalf("PeerAddrs[%d] = %q, want %q", i, spec.PeerAddrs[i], peerAddrs[i])
		}
	}
}

func TestAsTorrentRecord(t *testing.T) {
	record := storage.TorrentRecord{InfoHash: "abc"}

	if got, ok := asTorrentRecord(record); !ok || got.InfoHash != "abc" {
		t.Fatalf("值类型转换失败: %+v, %v", got, ok)
	}
	if got, ok := asTorrentRecord(&record); !ok || got.InfoHash != "abc" {
		t.Fatalf("指针类型转换失败: %+v, %v", got, ok)
	}
	if got, ok := asTorrentRecord(struct{}{}); ok {
		t.Fatalf("不支持的记录类型不应转换成功: %+v", got)
	}
}
