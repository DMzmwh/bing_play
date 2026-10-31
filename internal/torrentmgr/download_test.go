package torrentmgr

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bing/bing-play/internal/db/models"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// 同一目录必须复用同一个分片完成记录句柄：
// CGO 关闭时底层为 BoltDB，重复打开会触发文件锁 timeout，导致任务被误判为网络异常。
func TestPieceCompletionForDirShared(t *testing.T) {
	m := &TorrentManager{pieces: make(map[string]storage.PieceCompletion)}
	dir := t.TempDir()

	first, err := m.pieceCompletionForDir(dir)
	if err != nil {
		t.Fatalf("首次打开分片完成记录失败: %v", err)
	}
	second, err := m.pieceCompletionForDir(dir)
	if err != nil {
		t.Fatalf("复用分片完成记录失败: %v", err)
	}
	if first != second {
		t.Fatalf("同一目录应复用同一个分片完成记录句柄")
	}

	other, err := m.pieceCompletionForDir(t.TempDir())
	if err != nil {
		t.Fatalf("打开另一个目录的分片完成记录失败: %v", err)
	}
	if first == other {
		t.Fatalf("不同目录不应共享分片完成记录句柄")
	}

	_ = first.Close()
	_ = other.Close()
}

func TestDefaultDownloadConfig(t *testing.T) {
	cfg := DefaultDownloadConfig()

	if cfg.MaxConcurrent <= 0 {
		t.Fatalf("MaxConcurrent = %d, want > 0", cfg.MaxConcurrent)
	}
	if cfg.MaxRetries < 0 {
		t.Fatalf("MaxRetries = %d, want >= 0", cfg.MaxRetries)
	}
	if cfg.ProgressInterval <= 0 {
		t.Fatalf("ProgressInterval = %v, want > 0", cfg.ProgressInterval)
	}
	if cfg.PersistInterval <= 0 {
		t.Fatalf("PersistInterval = %v, want > 0", cfg.PersistInterval)
	}
	if cfg.MetadataTimeout <= 0 || cfg.RetryDelay <= 0 || cfg.StallTimeout <= 0 {
		t.Fatalf("时间参数必须为正数: %+v", cfg)
	}
	if cfg.StallTimeout < cfg.ProgressInterval {
		t.Fatalf("StallTimeout 应大于进度采样间隔: %v < %v", cfg.StallTimeout, cfg.ProgressInterval)
	}
}

func TestBatchDownloadResultAddBatchError(t *testing.T) {
	result := &BatchDownloadResult{}

	// 空 key 归入通用下载失败
	result.addBatchError("")
	if result.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", result.Failed)
	}
	if len(result.Errors) != 1 || result.Errors[0] != ErrKeyDownloadFailed {
		t.Fatalf("Errors = %v, want [%s]", result.Errors, ErrKeyDownloadFailed)
	}

	// 相同错误只记录一次，但失败计数仍然累加
	result.addBatchError(ErrKeyNetwork)
	result.addBatchError(ErrKeyNetwork)
	if result.Failed != 3 {
		t.Fatalf("Failed = %d, want 3", result.Failed)
	}
	if len(result.Errors) != 2 {
		t.Fatalf("Errors 应去重, got %v", result.Errors)
	}
}

func TestClassifyTorrentError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"permission", errors.New("open file: permission denied"), ErrKeyPermission},
		{"access denied", errors.New("Access is denied."), ErrKeyPermission},
		{"no space", errors.New("write: no space left on device"), ErrKeyNoSpace},
		{"gone", errors.New("open: no such file or directory"), ErrKeyResourceGone},
		{"network", errors.New("dial tcp: connection refused"), ErrKeyNetwork},
		{"timeout", errors.New("context deadline exceeded: timeout"), ErrKeyNetwork},
		{"other", errors.New("piece hash mismatch"), ErrKeyDownloadFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTorrentError(tc.err); got != tc.want {
				t.Fatalf("classifyTorrentError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestErrorKeyWrapped(t *testing.T) {
	wrapped := errors.New(ErrKeyMetadataTimeout + ": context deadline exceeded")
	if got := ErrorKey(wrapped); got != ErrKeyMetadataTimeout {
		t.Fatalf("ErrorKey = %q, want %q", got, ErrKeyMetadataTimeout)
	}
}

func TestEstimateSeconds(t *testing.T) {
	cases := []struct {
		name      string
		remaining int64
		speed     int64
		want      int64
	}{
		{"done", 0, 100, 0},
		{"negative", -1, 100, 0},
		{"unknown speed", 100, 0, -1},
		{"exact", 1000, 100, 10},
		{"ceil", 1001, 100, 11},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := estimateSeconds(tc.remaining, tc.speed); got != tc.want {
				t.Fatalf("estimateSeconds(%d, %d) = %d, want %d", tc.remaining, tc.speed, got, tc.want)
			}
		})
	}
}

func TestStatusRank(t *testing.T) {
	if taskStateRank("downloading") != 0 {
		t.Fatalf("下载中应排最前")
	}
	if taskStateRank("failed") >= taskStateRank("completed") {
		t.Fatalf("已完成任务应排在失败任务之后")
	}
	if taskStateRank("cancelled") <= taskStateRank("failed") {
		t.Fatalf("取消任务应排在最后")
	}
}

func TestDownloadConfigJSONDuration(t *testing.T) {
	// 保证配置中的时间字段为正数且可被前端按秒换算，避免默认值被误改。
	cfg := DefaultDownloadConfig()
	if got := cfg.MetadataTimeout / time.Second; got != 600 {
		t.Fatalf("MetadataTimeout = %d 秒, want 600", got)
	}
}

func TestIsVideoPath(t *testing.T) {
	cases := map[string]bool{
		"movie.mp4":    true,
		"Movie.MKV":    true,
		"subs/chs.ass": false,
		"readme.txt":   false,
		"":             false,
	}
	for path, want := range cases {
		if got := isVideoPath(path); got != want {
			t.Fatalf("isVideoPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestMetadataCachePath(t *testing.T) {
	if got := metadataCachePath("", "abc"); got != "" {
		t.Fatalf("空下载目录应返回空路径, got %q", got)
	}
	if got := metadataCachePath("/downloads", ""); got != "" {
		t.Fatalf("空 InfoHash 应返回空路径, got %q", got)
	}

	got := metadataCachePath("/downloads", "abcdef")
	want := filepath.Join("/downloads", metadataCacheDir, "abcdef.torrent")
	if got != want {
		t.Fatalf("metadataCachePath = %q, want %q", got, want)
	}
}

func TestMetadataCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()

	info := metainfo.Info{
		Name:        "test.bin",
		PieceLength: 16384,
		Pieces:      make([]byte, 20),
	}
	raw, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("编码测试元信息失败: %v", err)
	}
	mi := &metainfo.MetaInfo{InfoBytes: raw, Announce: "http://tracker.example/announce"}
	infoHash := mi.HashInfoBytes().HexString()

	if err := writeMetadataCache(dir, infoHash, mi); err != nil {
		t.Fatalf("写入元信息缓存失败: %v", err)
	}

	spec := readMetadataCache(dir, infoHash)
	if spec == nil {
		t.Fatal("应能读取到缓存的元信息")
	}
	if spec.InfoHash.HexString() != infoHash {
		t.Fatalf("InfoHash 不匹配: %q != %q", spec.InfoHash.HexString(), infoHash)
	}
	if len(spec.InfoBytes) == 0 {
		t.Fatal("缓存恢复的 spec 应包含 InfoBytes，避免重新拉取元信息")
	}
	if len(spec.Trackers) == 0 {
		t.Fatal("缓存恢复的 spec 应保留 Tracker")
	}

	// InfoHash 不匹配时不应复用缓存
	if got := readMetadataCache(dir, "ffffffffffffffffffffffffffffffffffffffff"); got != nil {
		t.Fatal("InfoHash 不匹配时应忽略缓存")
	}

	// 缓存损坏时不应复用，交由磁力链接回退
	path := metadataCachePath(dir, infoHash)
	if err := os.WriteFile(path, []byte("broken"), 0o644); err != nil {
		t.Fatalf("破坏缓存文件失败: %v", err)
	}
	if got := readMetadataCache(dir, infoHash); got != nil {
		t.Fatal("损坏的缓存应返回 nil")
	}
}

func TestVOFromResource(t *testing.T) {
	name := "示例资源"
	fileName := "movie.mp4"
	total := int64(1000)
	downloaded := int64(400)
	priority := 7
	created := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	verified := created.Add(time.Hour)
	resource := &models.Resource{
		InfoHash:       "hash-1",
		ResourceName:   &name,
		FileName:       &fileName,
		TotalSize:      &total,
		DownloadedSize: &downloaded,
		Kind:           models.KindTorrent,
		TaskState:      models.TaskPaused,
		LocalState:     models.LocalPartial,
		LocalPath:      "/downloads",
		Priority:       &priority,
		Attempt:        2,
		MaxAttempts:    3,
		ErrorCode:      ErrKeyNetwork,
		CreateTime:     &created,
		VerifiedAt:     &verified,
	}

	payload := voFromResource(resource)
	if payload.InfoHash != "hash-1" || payload.Name != name || payload.FileName != fileName {
		t.Fatalf("基础字段转换错误: %+v", payload)
	}
	if payload.TaskState != "paused" || payload.LocalState != "partial" || payload.DisplayState != "paused" {
		t.Fatalf("状态字段转换错误: %+v", payload)
	}
	if payload.Priority != priority || payload.Attempt != 2 || payload.MaxAttempts != 3 || payload.ErrorCode != ErrKeyNetwork {
		t.Fatalf("控制字段转换错误: %+v", payload)
	}
	if payload.TotalSize != total || payload.Downloaded != downloaded {
		t.Fatalf("体积字段转换错误: %+v", payload)
	}
	if payload.Progress != 40 {
		t.Fatalf("Progress = %v, want 40", payload.Progress)
	}
	if payload.EtaSeconds != -1 {
		t.Fatalf("EtaSeconds = %d, want -1", payload.EtaSeconds)
	}
	if payload.CreatedTime != created.Format(time.DateTime) || payload.VerifiedAt != verified.Format(time.DateTime) {
		t.Fatalf("时间格式转换错误: %+v", payload)
	}

	// 已完成任务应固定为 100%
	done := *resource
	done.TaskState = models.TaskCompleted
	done.LocalState = models.LocalPresent
	donePayload := voFromResource(&done)
	if donePayload.Progress != 100 || donePayload.EtaSeconds != 0 {
		t.Fatalf("已完成任务进度 = %v, eta = %d", donePayload.Progress, donePayload.EtaSeconds)
	}
	if donePayload.DisplayState != "completed" {
		t.Fatalf("已完成任务展示态 = %q, want completed", donePayload.DisplayState)
	}
}
