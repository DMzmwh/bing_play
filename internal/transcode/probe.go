package transcode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
)

// Prober 探测文件格式。实现必须保证只读、快速返回。
type Prober interface {
	Probe(ctx context.Context, path string) (Format, error)
}

// Detector 组合扩展名、MP4 结构解析与 ffprobe 的探测实现。
//
// ffprobe 能给出编码为准的完整信息；不可用时退回扩展名 + MP4 结构解析，
// 保证探测环节永远不因缺少外部程序而失败。
type Detector struct {
	Binaries Binaries
	Runner   Runner
}

// NewDetector 创建探测器。
func NewDetector(binaries Binaries, runner Runner) *Detector {
	return &Detector{Binaries: binaries, Runner: runner}
}

// Probe 实现 Prober。
func (d *Detector) Probe(ctx context.Context, path string) (Format, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return Format{}, err
	}

	format := FormatFromPath(path, stat.Size())

	if format.Container.IsMP4Family() {
		if info, err := sniffMP4(path); err == nil {
			if info.Container != "" {
				format.Container = info.Container
			}
			format.VideoCodec = info.primaryVideoCodec()
			format.AudioCodec = info.primaryAudioCodec()
			format.HasVideo = format.VideoCodec != CodecUnknown
			format.HasAudio = format.AudioCodec != CodecUnknown
		}
	}

	if d.Binaries.FFprobe == "" || d.Runner == nil {
		return format, nil
	}

	out, probeErr := d.Runner.Output(ctx,
		d.Binaries.FFprobe,
		[]string{
			"-v", "error",
			"-show_streams",
			"-show_format",
			"-of", "json",
			path,
		},
	)
	if probeErr != nil {
		// ffprobe 失败不影响已经拿到的结构信息。
		return format, nil
	}

	var parsed probeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return format, nil
	}
	return applyProbeOutput(format, parsed), nil
}

type probeOutput struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	CodecType        string `json:"codec_type"`
	CodecName        string `json:"codec_name"`
	Profile          string `json:"profile"`
	PixFmt           string `json:"pix_fmt"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	BitsPerRawSample string `json:"bits_per_raw_sample"`
	Duration         string `json:"duration"`
}

type probeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
}

func applyProbeOutput(format Format, out probeOutput) Format {
	for _, stream := range out.Streams {
		switch stream.CodecType {
		case "video":
			if codec := codecFromFFmpegName(stream.CodecName); codec.IsVideo() {
				format.VideoCodec = codec
				format.HasVideo = true
			}
			if format.Profile == "" {
				format.Profile = stream.Profile
			}
			if format.PixFmt == "" {
				format.PixFmt = stream.PixFmt
			}
			if format.Width == 0 {
				format.Width = stream.Width
				format.Height = stream.Height
			}
			if format.BitDepth == 0 {
				format.BitDepth = bitDepthOf(stream.PixFmt, stream.BitsPerRawSample)
			}
		case "audio":
			format.AudioCodec = codecFromFFmpegName(stream.CodecName)
			format.HasAudio = true
		}
	}

	if format.DurationMS == 0 {
		format.DurationMS = parseDurationMS(out.Format.Duration)
	}
	if format.DurationMS == 0 {
		for _, stream := range out.Streams {
			if ms := parseDurationMS(stream.Duration); ms > 0 {
				format.DurationMS = ms
				break
			}
		}
	}

	if format.Container == ContainerUnknown {
		format.Container = containerFromFFmpegName(out.Format.FormatName)
	}
	return format
}

func bitDepthOf(pixFmt, bitsPerRawSample string) int {
	if depth, err := strconv.Atoi(strings.TrimSpace(bitsPerRawSample)); err == nil && depth > 0 {
		return depth
	}
	pix := strings.ToLower(pixFmt)
	switch {
	case strings.Contains(pix, "10"):
		return 10
	case strings.Contains(pix, "12"):
		return 12
	case pix != "":
		return 8
	}
	return 0
}

func parseDurationMS(value string) int64 {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return int64(seconds * 1000)
}

func containerFromFFmpegName(name string) Container {
	switch {
	case strings.Contains(name, "matroska"):
		return ContainerMKV
	case strings.Contains(name, "webm"):
		return ContainerWebM
	case strings.Contains(name, "avi"):
		return ContainerAVI
	case strings.Contains(name, "flv"):
		return ContainerFLV
	case strings.Contains(name, "mpegts"):
		return ContainerTS
	case strings.Contains(name, "mov"):
		return ContainerMP4
	default:
		return ContainerUnknown
	}
}

// DetectFile 是 Detector 的便捷封装。
func DetectFile(ctx context.Context, path string, binaries Binaries, runner Runner) (Format, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return Format{}, err
	}
	if stat.IsDir() {
		return Format{}, errors.New("transcode: path is a directory")
	}
	return NewDetector(binaries, runner).Probe(ctx, path)
}
