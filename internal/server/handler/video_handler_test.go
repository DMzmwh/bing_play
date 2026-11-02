package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/transcode"
)

func TestLocalFilePath(t *testing.T) {
	dir := t.TempDir()
	fileName := "movie.mp4"
	videoPath := filepath.Join(dir, fileName)
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}
	subName := filepath.Join("Season 1", "episode.mkv")
	subPath := filepath.Join(dir, subName)
	if err := os.MkdirAll(filepath.Dir(subPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(subPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}
	empty := ""
	missing := "missing.mp4"

	cases := []struct {
		name     string
		resource models.Resource
		wantPath string
		wantKey  string
		wantCode int
	}{
		{
			name:     "空路径",
			resource: models.Resource{},
			wantKey:  "video.error_empty_path",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "缺少文件名",
			resource: models.Resource{LocalPath: dir},
			wantKey:  "video.error_file_required",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "文件名为空",
			resource: models.Resource{LocalPath: dir, FileName: &empty},
			wantKey:  "video.error_file_required",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "文件不存在",
			resource: models.Resource{LocalPath: dir, FileName: &missing},
			wantKey:  "video.error_not_found",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "目录资源按记录文件名解析",
			resource: models.Resource{LocalPath: dir, FileName: &fileName},
			wantPath: videoPath,
		},
		{
			name:     "子目录文件按相对路径解析",
			resource: models.Resource{LocalPath: dir, FileName: &subName},
			wantPath: subPath,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path, key, code := localFilePath(&testCase.resource)
			if path != testCase.wantPath || key != testCase.wantKey || code != testCase.wantCode {
				t.Fatalf(
					"localFilePath() = (%q, %q, %d), want (%q, %q, %d)",
					path, key, code, testCase.wantPath, testCase.wantKey, testCase.wantCode,
				)
			}
		})
	}
}

func TestTranscodeReasonKey(t *testing.T) {
	cases := []struct {
		reason transcode.Reason
		want   string
	}{
		{transcode.ReasonHEVCDecoderMissing, "video.transcode.reason.hevc_decoder_missing"},
		{transcode.ReasonContainerUnsupported, "video.transcode.reason.container_unsupported"},
		{transcode.ReasonVideoUnsupported, "video.transcode.reason.video_codec_unsupported"},
		{transcode.ReasonAudioUnsupported, "video.transcode.reason.audio_codec_unsupported"},
		{transcode.ReasonHighBitDepth, "video.transcode.reason.high_bit_depth"},
		{transcode.ReasonFFmpegMissing, "video.transcode.error.ffmpeg_missing"},
		{transcode.ReasonInsufficientDisk, "video.transcode.error.insufficient_disk"},
		{transcode.ReasonMeteredNetwork, "video.transcode.error.metered_network"},
		{transcode.ReasonProbeFailed, "video.transcode.error.probe_failed"},
		{transcode.ReasonCanceled, "video.transcode.error.canceled"},
		{transcode.ReasonClosed, "video.transcode.error.closed"},
		{transcode.ReasonConvertFailed, "video.transcode.error.convert_failed"},
		{transcode.Reason(""), "video.transcode.error.convert_failed"},
	}

	for _, testCase := range cases {
		if got := transcodeReasonKey(testCase.reason); got != testCase.want {
			t.Fatalf("transcodeReasonKey(%q) = %q, want %q", testCase.reason, got, testCase.want)
		}
	}
}
