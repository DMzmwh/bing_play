package transcode

import "testing"

func hevcFormat(size int64) Format {
	return Format{
		Container:  ContainerMP4,
		VideoCodec: CodecHEVC,
		AudioCodec: CodecAAC,
		HasVideo:   true,
		HasAudio:   true,
		SizeBytes:  size,
	}
}

func goodConditions() Conditions {
	return Conditions{
		FFmpegAvailable: true,
		FreeDiskBytes:   10 << 30,
		Network:         NetworkLocal,
		HardwareEncoder: true,
	}
}

func TestDecide(t *testing.T) {
	t.Run("可直接播放时不转换", func(t *testing.T) {
		h264 := Format{
			Container:  ContainerMP4,
			VideoCodec: CodecH264,
			AudioCodec: CodecAAC,
			HasVideo:   true,
			HasAudio:   true,
		}
		decision := Decide(h264, goodConditions(), DefaultPolicy())
		if decision.Action != ActionPlayDirect {
			t.Fatalf("action = %s, want play_direct", decision.Action)
		}
	})

	t.Run("不兼容编码需要转换", func(t *testing.T) {
		decision := Decide(hevcFormat(10<<20), goodConditions(), DefaultPolicy())
		if decision.Action != ActionConvert {
			t.Fatalf("action = %s, want convert (%s)", decision.Action, decision.Reason)
		}
		if !decision.UseHardware {
			t.Fatal("expected hardware encoder to be preferred")
		}
		if decision.Reason != ReasonHEVCDecoderMissing {
			t.Fatalf("reason = %s", decision.Reason)
		}
	})

	t.Run("缺少 ffmpeg 时回退", func(t *testing.T) {
		cond := goodConditions()
		cond.FFmpegAvailable = false
		decision := Decide(hevcFormat(10<<20), cond, DefaultPolicy())
		if decision.Action != ActionFallback {
			t.Fatalf("action = %s, want fallback", decision.Action)
		}
		if decision.Reason != ReasonFFmpegMissing {
			t.Fatalf("reason = %s", decision.Reason)
		}
	})

	t.Run("计量网络下推迟", func(t *testing.T) {
		cond := goodConditions()
		cond.Network = NetworkMetered
		decision := Decide(hevcFormat(10<<20), cond, DefaultPolicy())
		if decision.Action != ActionDefer {
			t.Fatalf("action = %s, want defer", decision.Action)
		}
	})

	t.Run("磁盘不足时回退", func(t *testing.T) {
		cond := goodConditions()
		cond.FreeDiskBytes = 1 << 20
		decision := Decide(hevcFormat(1<<30), cond, DefaultPolicy())
		if decision.Action != ActionFallback {
			t.Fatalf("action = %s, want fallback", decision.Action)
		}
		if decision.Reason != ReasonInsufficientDisk {
			t.Fatalf("reason = %s", decision.Reason)
		}
	})

	t.Run("大文件在关闭硬件偏好时仍走硬件", func(t *testing.T) {
		policy := DefaultPolicy()
		policy.PreferHardware = false
		policy.HardwareAboveBytes = 100 << 20

		decision := Decide(hevcFormat(200<<20), goodConditions(), policy)
		if !decision.UseHardware {
			t.Fatal("large file should use hardware encoder")
		}

		decision = Decide(hevcFormat(10<<20), goodConditions(), policy)
		if decision.UseHardware {
			t.Fatal("small file should not use hardware encoder")
		}
	})

	t.Run("强制转换忽略兼容性", func(t *testing.T) {
		h264 := Format{Container: ContainerMP4, VideoCodec: CodecH264, HasVideo: true}
		policy := DefaultPolicy()
		policy.AlwaysConvert = true

		if decision := Decide(h264, goodConditions(), policy); decision.Action != ActionConvert {
			t.Fatalf("action = %s, want convert", decision.Action)
		}
	})
}
