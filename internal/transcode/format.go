package transcode

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Container 封装容器类型。
type Container string

const (
	ContainerUnknown   Container = "unknown"
	ContainerMP4       Container = "mp4"
	ContainerMOV       Container = "mov"
	ContainerM4V       Container = "m4v"
	ContainerMKV       Container = "mkv"
	ContainerWebM      Container = "webm"
	ContainerAVI       Container = "avi"
	ContainerFLV       Container = "flv"
	ContainerTS        Container = "ts"
	ContainerMPEG      Container = "mpeg"
	ContainerOGG       Container = "ogg"
	ContainerThreeGP   Container = "3gp"
	ContainerWMV       Container = "wmv"
	ContainerRealMedia Container = "rm"
)

// Codec 音视频编码。
type Codec string

const (
	CodecUnknown   Codec = "unknown"
	CodecH264      Codec = "h264"
	CodecHEVC      Codec = "hevc"
	CodecAV1       Codec = "av1"
	CodecVP9       Codec = "vp9"
	CodecVP8       Codec = "vp8"
	CodecMPEG4     Codec = "mpeg4"
	CodecMPEG2     Codec = "mpeg2"
	CodecVC1       Codec = "vc1"
	CodecWMV3      Codec = "wmv3"
	CodecProRes    Codec = "prores"
	CodecTheora    Codec = "theora"
	CodecRealVideo Codec = "realvideo"
	CodecAAC       Codec = "aac"
	CodecMP3       Codec = "mp3"
	CodecOpus      Codec = "opus"
	CodecVorbis    Codec = "vorbis"
	CodecFLAC      Codec = "flac"
	CodecAC3       Codec = "ac3"
	CodecEAC3      Codec = "eac3"
	CodecDTS       Codec = "dts"
	CodecTrueHD    Codec = "truehd"
	CodecALAC      Codec = "alac"
	CodecPCM       Codec = "pcm"
)

// IsVideo 判断编码是否属于视频编码。
func (c Codec) IsVideo() bool {
	switch c {
	case CodecH264, CodecHEVC, CodecAV1, CodecVP9, CodecVP8,
		CodecMPEG4, CodecMPEG2, CodecVC1, CodecWMV3, CodecProRes,
		CodecTheora, CodecRealVideo:
		return true
	default:
		return false
	}
}

// IsAudio 判断编码是否属于音频编码。
func (c Codec) IsAudio() bool {
	switch c {
	case CodecAAC, CodecMP3, CodecOpus, CodecVorbis, CodecFLAC,
		CodecAC3, CodecEAC3, CodecDTS, CodecTrueHD, CodecALAC, CodecPCM:
		return true
	default:
		return false
	}
}

// Format 视频文件的格式信息。
//
// 扩展名只能给出容器类型，编码信息来自 MP4 结构解析或 ffprobe。
type Format struct {
	Path       string    `json:"path"`
	FileName   string    `json:"fileName"`
	Container  Container `json:"container"`
	VideoCodec Codec     `json:"videoCodec"`
	AudioCodec Codec     `json:"audioCodec"`
	Profile    string    `json:"profile"`
	PixFmt     string    `json:"pixFmt"`
	BitDepth   int       `json:"bitDepth"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	DurationMS int64     `json:"durationMs"`
	SizeBytes  int64     `json:"sizeBytes"`
	HasVideo   bool      `json:"hasVideo"`
	HasAudio   bool      `json:"hasAudio"`
}

var containerByExtension = map[string]Container{
	".mp4":  ContainerMP4,
	".m4v":  ContainerM4V,
	".mov":  ContainerMOV,
	".qt":   ContainerMOV,
	".mkv":  ContainerMKV,
	".mk3d": ContainerMKV,
	".webm": ContainerWebM,
	".avi":  ContainerAVI,
	".flv":  ContainerFLV,
	".f4v":  ContainerFLV,
	".ts":   ContainerTS,
	".mts":  ContainerTS,
	".m2ts": ContainerTS,
	".mpeg": ContainerMPEG,
	".mpg":  ContainerMPEG,
	".mpe":  ContainerMPEG,
	".m1v":  ContainerMPEG,
	".m2v":  ContainerMPEG,
	".vob":  ContainerMPEG,
	".ogv":  ContainerOGG,
	".3gp":  ContainerThreeGP,
	".3g2":  ContainerThreeGP,
	".wmv":  ContainerWMV,
	".asf":  ContainerWMV,
	".rm":   ContainerRealMedia,
	".rmvb": ContainerRealMedia,
}

// FormatFromPath 根据扩展名推断容器类型，编码信息此时仍未知。
func FormatFromPath(path string, size int64) Format {
	container, ok := containerByExtension[strings.ToLower(filepath.Ext(path))]
	if !ok {
		container = ContainerUnknown
	}
	return Format{
		Path:      path,
		FileName:  filepath.Base(path),
		Container: container,
		SizeBytes: size,
	}
}

// IsMP4Family 表示该容器基于 ISO BMFF，可以用统一的结构解析器读取编码信息。
func (c Container) IsMP4Family() bool {
	switch c {
	case ContainerMP4, ContainerMOV, ContainerM4V, ContainerThreeGP:
		return true
	default:
		return false
	}
}

// codecFromFourCC 把 MP4 sample entry 的 fourcc 映射为编码。
func codecFromFourCC(tag string) Codec {
	switch tag {
	case "avc1", "avc3", "avc2", "avc4":
		return CodecH264
	case "hvc1", "hev1", "dvhe", "dvh1":
		return CodecHEVC
	case "av01":
		return CodecAV1
	case "vp09":
		return CodecVP9
	case "vp08":
		return CodecVP8
	case "mp4v":
		return CodecMPEG4
	case "mp4a":
		return CodecAAC
	case "ac-3":
		return CodecAC3
	case "ec-3":
		return CodecEAC3
	case "Opus":
		return CodecOpus
	case "fLaC":
		return CodecFLAC
	case "alac":
		return CodecALAC
	case ".mp3":
		return CodecMP3
	case "dtsc", "dtsh", "dtse":
		return CodecDTS
	case "sowt", "twos", "lpcm", "in24", "in32":
		return CodecPCM
	default:
		return CodecUnknown
	}
}

// codecFromFFmpegName 把 ffprobe 的 codec_name 映射为编码。
func codecFromFFmpegName(name string) Codec {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "h264":
		return CodecH264
	case "hevc":
		return CodecHEVC
	case "av1":
		return CodecAV1
	case "vp9":
		return CodecVP9
	case "vp8":
		return CodecVP8
	case "mpeg4":
		return CodecMPEG4
	case "mpeg2video":
		return CodecMPEG2
	case "vc1":
		return CodecVC1
	case "wmv3":
		return CodecWMV3
	case "prores":
		return CodecProRes
	case "theora":
		return CodecTheora
	case "rv30", "rv40":
		return CodecRealVideo
	case "aac":
		return CodecAAC
	case "mp3", "mp3float":
		return CodecMP3
	case "opus":
		return CodecOpus
	case "vorbis":
		return CodecVorbis
	case "flac":
		return CodecFLAC
	case "ac3":
		return CodecAC3
	case "eac3":
		return CodecEAC3
	case "dts":
		return CodecDTS
	case "truehd":
		return CodecTrueHD
	case "alac":
		return CodecALAC
	}
	if strings.HasPrefix(name, "pcm_") {
		return CodecPCM
	}
	return CodecUnknown
}

// Target 目标播放内核。
type Target string

const (
	TargetWebView2  Target = "webview2"
	TargetWKWebView Target = "wkwebview"
	TargetWebKitGTK Target = "webkitgtk"
)

// TargetForOS 返回当前平台的默认播放内核。
func TargetForOS(goos string) Target {
	switch goos {
	case "windows":
		return TargetWebView2
	case "darwin":
		return TargetWKWebView
	default:
		return TargetWebKitGTK
	}
}

// Reason 播放兼容性判定原因 / 转换决策原因。
type Reason string

const (
	ReasonCompatible           Reason = "compatible"
	ReasonContainerUnsupported Reason = "container_unsupported"
	ReasonVideoUnsupported     Reason = "video_codec_unsupported"
	ReasonHEVCDecoderMissing   Reason = "hevc_decoder_missing"
	ReasonHighBitDepth         Reason = "high_bit_depth"
	ReasonAudioUnsupported     Reason = "audio_codec_unsupported"
	ReasonFFmpegMissing        Reason = "ffmpeg_missing"
	ReasonInsufficientDisk     Reason = "insufficient_disk"
	ReasonMeteredNetwork       Reason = "metered_network"
	ReasonProbeFailed          Reason = "probe_failed"
	ReasonConvertFailed        Reason = "convert_failed"
	ReasonCanceled             Reason = "canceled"
	ReasonClosed               Reason = "scheduler_closed"
)

// Compatibility 当前目标内核能否直接播放该文件。
type Compatibility struct {
	Playable bool     `json:"playable"`
	Reasons  []Reason `json:"reasons"`
}

var supportedContainers = map[Container]struct{}{
	ContainerMP4:     {},
	ContainerMOV:     {},
	ContainerM4V:     {},
	ContainerThreeGP: {},
	ContainerWebM:    {},
}

var supportedAudioCodecs = map[Codec]struct{}{
	CodecAAC:    {},
	CodecMP3:    {},
	CodecOpus:   {},
	CodecVorbis: {},
	CodecFLAC:   {},
}

// Evaluate 判断文件在目标内核上能否直接播放。
//
// 这里按 Chromium / WebKit 的公开能力建模：
//   - WebView2（Chromium）不内置 HEVC 解码器，依赖系统扩展；
//   - WKWebView 走系统 VideoToolbox，支持 HEVC；
//   - WebKitGTK 取决于系统 GStreamer，按不支持 HEVC 处理。
func Evaluate(f Format, target Target) Compatibility {
	if target == "" {
		target = TargetForOS(runtime.GOOS)
	}

	compat := Compatibility{Playable: true}

	if _, ok := supportedContainers[f.Container]; !ok {
		compat.Playable = false
		compat.Reasons = append(compat.Reasons, ReasonContainerUnsupported)
	}

	if !f.HasVideo && f.VideoCodec == CodecUnknown {
		compat.Playable = false
		compat.Reasons = append(compat.Reasons, ReasonVideoUnsupported)
	}

	if !videoCodecSupported(f, target) {
		compat.Playable = false
		compat.Reasons = append(compat.Reasons, videoReason(f, target))
	}

	if f.HasAudio && !audioCodecSupported(f.AudioCodec) {
		compat.Playable = false
		compat.Reasons = append(compat.Reasons, ReasonAudioUnsupported)
	}

	if compat.Playable {
		compat.Reasons = []Reason{ReasonCompatible}
	}
	return compat
}

func videoCodecSupported(f Format, target Target) bool {
	switch f.VideoCodec {
	case CodecH264:
		return h264PixelFormatSupported(f)
	case CodecHEVC:
		// WKWebView 通过系统 VideoToolbox 解码 HEVC。
		return target == TargetWKWebView
	case CodecAV1:
		// WebView2 已支持 AV1；WebKitGTK 与 WKWebView 视系统而定，按不支持处理。
		return target == TargetWebView2
	case CodecVP9, CodecVP8:
		return true
	default:
		// 未知编码无法保证可播，一律按不支持处理，交给转换流程。
		return false
	}
}

func videoReason(f Format, target Target) Reason {
	switch f.VideoCodec {
	case CodecHEVC:
		if target != TargetWKWebView {
			return ReasonHEVCDecoderMissing
		}
	case CodecH264:
		if !h264PixelFormatSupported(f) {
			return ReasonHighBitDepth
		}
	}
	return ReasonVideoUnsupported
}

// h264PixelFormatSupported Chromium 的 H.264 只保证支持 8bit yuv420p。
func h264PixelFormatSupported(f Format) bool {
	if f.BitDepth > 8 {
		return false
	}
	pix := strings.ToLower(f.PixFmt)
	if pix == "" {
		return true
	}
	if pix == "yuv420p" || pix == "yuvj420p" {
		return true
	}
	for _, unsupported := range []string{"10", "12", "422", "444"} {
		if strings.Contains(pix, unsupported) {
			return false
		}
	}
	return true
}

func audioCodecSupported(c Codec) bool {
	if c == CodecUnknown {
		// 音频未知时不阻断播放：最坏情况是无声，不应把整个视频判成不可播。
		return true
	}
	_, ok := supportedAudioCodecs[c]
	return ok
}
