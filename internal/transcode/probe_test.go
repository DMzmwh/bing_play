package transcode

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestDetectorUsesFFprobe(t *testing.T) {
	path := writeTemp(t, "sample.mp4", buildMP4("isom", "hvc1", "mp4a"))

	output, err := json.Marshal(map[string]any{
		"streams": []map[string]any{
			{
				"codec_type": "video",
				"codec_name": "hevc",
				"profile":    "Main",
				"pix_fmt":    "yuv420p",
				"width":      1280,
				"height":     720,
			},
			{"codec_type": "audio", "codec_name": "aac"},
		},
		"format": map[string]any{
			"format_name": "mov,mp4,m4a,3gp,3g2,mj2",
			"duration":    "12.5",
		},
	})
	if err != nil {
		t.Fatalf("marshal probe output: %v", err)
	}

	detector := NewDetector(
		Binaries{FFprobe: "ffprobe"},
		&fakeRunner{probeOutput: output},
	)
	format, err := detector.Probe(context.Background(), path)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if format.VideoCodec != CodecHEVC {
		t.Fatalf("video codec = %s, want hevc", format.VideoCodec)
	}
	if format.AudioCodec != CodecAAC {
		t.Fatalf("audio codec = %s, want aac", format.AudioCodec)
	}
	if format.DurationMS != 12500 {
		t.Fatalf("duration = %d, want 12500", format.DurationMS)
	}
}

func TestDetectorFallsBackToMP4Structure(t *testing.T) {
	path := writeTemp(t, "sample.mp4", buildMP4("isom", "avc1", "mp4a"))

	detector := NewDetector(Binaries{}, nil)
	format, err := detector.Probe(context.Background(), path)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if format.VideoCodec != CodecH264 {
		t.Fatalf("video codec = %s, want h264", format.VideoCodec)
	}
	if !format.HasVideo || !format.HasAudio {
		t.Fatal("expected video and audio streams to be detected")
	}
}

func TestDetectorRejectsMissingFile(t *testing.T) {
	_, err := NewDetector(Binaries{}, nil).Probe(
		context.Background(),
		filepath.Join(t.TempDir(), "missing.mp4"),
	)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
