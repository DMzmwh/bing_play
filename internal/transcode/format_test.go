package transcode

import "testing"

func TestFormatFromPath(t *testing.T) {
	cases := map[string]Container{
		"a.mp4":  ContainerMP4,
		"a.mov":  ContainerMOV,
		"a.mkv":  ContainerMKV,
		"a.webm": ContainerWebM,
		"a.avi":  ContainerAVI,
		"a.flv":  ContainerFLV,
		"a.ts":   ContainerTS,
		"a.rmvb": ContainerRealMedia,
		"a.txt":  ContainerUnknown,
	}

	for name, want := range cases {
		if got := FormatFromPath(name, 0).Container; got != want {
			t.Errorf("%s container = %s, want %s", name, got, want)
		}
	}
}

func TestEvaluateCompatibility(t *testing.T) {
	h264 := Format{
		Container:  ContainerMP4,
		VideoCodec: CodecH264,
		AudioCodec: CodecAAC,
		PixFmt:     "yuv420p",
		HasVideo:   true,
		HasAudio:   true,
	}

	t.Run("mp4 h264 aac 可直接播放", func(t *testing.T) {
		compat := Evaluate(h264, TargetWebView2)
		if !compat.Playable {
			t.Fatalf("unexpected reasons: %v", compat.Reasons)
		}
	})

	t.Run("hevc 在 WebView2 上不可播", func(t *testing.T) {
		hevc := h264
		hevc.VideoCodec = CodecHEVC
		compat := Evaluate(hevc, TargetWebView2)
		if compat.Playable {
			t.Fatal("hevc should not be playable on webview2")
		}
		if len(compat.Reasons) == 0 || compat.Reasons[0] != ReasonHEVCDecoderMissing {
			t.Fatalf("reasons = %v, want hevc_decoder_missing", compat.Reasons)
		}
	})

	t.Run("hevc 在 WKWebView 上可播", func(t *testing.T) {
		hevc := h264
		hevc.VideoCodec = CodecHEVC
		if compat := Evaluate(hevc, TargetWKWebView); !compat.Playable {
			t.Fatalf("unexpected reasons: %v", compat.Reasons)
		}
	})

	t.Run("mkv 容器不可播", func(t *testing.T) {
		mkv := h264
		mkv.Container = ContainerMKV
		compat := Evaluate(mkv, TargetWebView2)
		if compat.Playable {
			t.Fatal("mkv should not be playable")
		}
		if compat.Reasons[0] != ReasonContainerUnsupported {
			t.Fatalf("reasons = %v", compat.Reasons)
		}
	})

	t.Run("10bit h264 不可播", func(t *testing.T) {
		deep := h264
		deep.PixFmt = "yuv420p10le"
		deep.BitDepth = 10
		compat := Evaluate(deep, TargetWebView2)
		if compat.Playable {
			t.Fatal("10bit h264 should not be playable")
		}
		if compat.Reasons[0] != ReasonHighBitDepth {
			t.Fatalf("reasons = %v", compat.Reasons)
		}
	})

	t.Run("ac3 音频不可播", func(t *testing.T) {
		ac3 := h264
		ac3.AudioCodec = CodecAC3
		compat := Evaluate(ac3, TargetWebView2)
		if compat.Playable {
			t.Fatal("ac3 audio should not be playable")
		}
		if compat.Reasons[0] != ReasonAudioUnsupported {
			t.Fatalf("reasons = %v", compat.Reasons)
		}
	})

	t.Run("未知视频编码按不可播处理", func(t *testing.T) {
		unknown := Format{Container: ContainerMP4}
		if compat := Evaluate(unknown, TargetWebView2); compat.Playable {
			t.Fatal("unknown codec should not be playable")
		}
	})
}
