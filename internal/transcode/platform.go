package transcode

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Binaries 记录 ffmpeg / ffprobe 的可执行文件路径。
type Binaries struct {
	FFmpeg  string `json:"ffmpeg"`
	FFprobe string `json:"ffprobe"`
}

// Available 是否具备执行转码的最低条件。
func (b Binaries) Available() bool {
	return b.FFmpeg != ""
}

// binName 返回当前平台的可执行文件名。
func binName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// platformDir 返回随包二进制的目录名，例如 windows-amd64。
func platformDir() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// LocateBinaries 按以下顺序查找 ffmpeg / ffprobe：
//
//  1. 可执行文件同级的 resources/bin/ffmpeg/<goos>-<goarch>/
//  2. 工作目录下的 resources/bin/ffmpeg/<goos>-<goarch>/
//  3. resources/bin/ffmpeg/（单平台平铺布局）
//  4. 系统 PATH
//
// 第 1、2 步对应项目约定；第 3 步便于只分发单平台时直接平铺；
// 第 4 步作为兜底，让用户自行安装的 ffmpeg 也能用。
func LocateBinaries() Binaries {
	dirs := binarySearchDirs()

	var bins Binaries
	for _, dir := range dirs {
		if bins.FFmpeg == "" {
			bins.FFmpeg = findBinary(dir, "ffmpeg")
		}
		if bins.FFprobe == "" {
			bins.FFprobe = findBinary(dir, "ffprobe")
		}
		if bins.Available() && bins.FFprobe != "" {
			return bins
		}
	}

	if bins.FFmpeg == "" {
		if p, err := exec.LookPath(binName("ffmpeg")); err == nil {
			bins.FFmpeg = p
		}
	}
	if bins.FFprobe == "" {
		if p, err := exec.LookPath(binName("ffprobe")); err == nil {
			bins.FFprobe = p
		}
	}
	return bins
}

func binarySearchDirs() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "resources", "bin", "ffmpeg", platformDir()))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(cwd, "resources", "bin", "ffmpeg", platformDir()))
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "resources", "bin", "ffmpeg"))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(cwd, "resources", "bin", "ffmpeg"))
	}
	return dirs
}

func findBinary(dir, base string) string {
	path := filepath.Join(dir, binName(base))
	if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
		return path
	}
	return ""
}

// hardwareEncoderArgs 返回当前平台首选的硬件编码参数。
//
// Windows 使用 NVENC，macOS 使用 VideoToolbox；Linux 下 NVENC 依赖驱动，
// 保守起见不默认尝试。硬件编码失败时由调度器自动回退到软件编码。
func hardwareEncoderArgs() ([]string, bool) {
	switch runtime.GOOS {
	case "windows":
		return []string{"-c:v", "h264_nvenc", "-preset", "p5", "-cq", "23"}, true
	case "darwin":
		return []string{"-c:v", "h264_videotoolbox", "-b:v", "3M"}, true
	default:
		return nil, false
	}
}

// softwareEncoderArgs 返回兼容性最好的软件编码参数。
func softwareEncoderArgs() []string {
	return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-profile:v", "high"}
}

// transcodeArgs 拼接一次完整的转码命令。
//
// 输出统一为 H.264 + AAC 的 MP4，并开启 faststart 以便边下边播。
func transcodeArgs(src, dst string, f Format, encoder []string) []string {
	args := []string{
		"-hide_banner",
		"-nostdin",
		"-y",
		"-i", src,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-map_metadata", "0",
		"-sn",
		"-dn",
	}

	args = append(args, encoder...)
	args = append(args, "-pix_fmt", "yuv420p")

	// AAC / MP3 直接复制，避免二次编码损失与额外耗时。
	if f.AudioCodec == CodecAAC || f.AudioCodec == CodecMP3 {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}

	// 输出先写到 .part 临时文件，扩展名不可靠，显式指定 mp4 封装。
	args = append(args,
		"-f", "mp4",
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		"-nostats",
		dst,
	)
	return args
}

// looksLikeHardwareArgs 判断参数是否使用硬件编码器，仅用于日志与测试。
func looksLikeHardwareArgs(args []string) bool {
	for _, arg := range args {
		if strings.HasSuffix(arg, "_nvenc") || strings.HasSuffix(arg, "_videotoolbox") {
			return true
		}
	}
	return false
}
