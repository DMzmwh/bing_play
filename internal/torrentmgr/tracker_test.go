package torrentmgr

import (
	"testing"

	"github.com/anacrolix/torrent"
)

func TestMergePublicTrackers(t *testing.T) {
	t.Run("空列表时全部追加", func(t *testing.T) {
		spec := &torrent.TorrentSpec{}
		mergePublicTrackers(spec)
		if len(spec.Trackers) != len(publicTrackers) {
			t.Fatalf("trackers = %d, want %d", len(spec.Trackers), len(publicTrackers))
		}
	})

	t.Run("已有公共 tracker 不重复追加", func(t *testing.T) {
		spec := &torrent.TorrentSpec{Trackers: [][]string{{publicTrackers[0]}}}
		mergePublicTrackers(spec)
		if len(spec.Trackers) != len(publicTrackers) {
			t.Fatalf("trackers = %d, want %d", len(spec.Trackers), len(publicTrackers))
		}
		if spec.Trackers[0][0] != publicTrackers[0] {
			t.Fatalf("原有 tracker 被改动: %v", spec.Trackers[0])
		}
	})

	t.Run("种子自带 tracker 保留", func(t *testing.T) {
		spec := &torrent.TorrentSpec{Trackers: [][]string{{"http://example.com/announce"}}}
		mergePublicTrackers(spec)
		if spec.Trackers[0][0] != "http://example.com/announce" {
			t.Fatalf("原有 tracker 丢失: %v", spec.Trackers[0])
		}
		if len(spec.Trackers) != len(publicTrackers)+1 {
			t.Fatalf("trackers = %d, want %d", len(spec.Trackers), len(publicTrackers)+1)
		}
	})
}
