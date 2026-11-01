package transcode

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
)

// mp4Info 是 MP4 结构解析的结果。
type mp4Info struct {
	Container   Container
	VideoCodecs []Codec
	AudioCodecs []Codec
	// HasMoov 表示文件包含 moov。
	HasMoov bool
}

// sniffMP4 只读取 MP4 的容器与 sample entry 信息，不依赖外部程序。
//
// 解析 ftyp（容器品牌）与 moov/trak/mdia/minf/stbl/stsd（编码 fourcc），
// 这样即使 ffprobe 不可用，也能判断视频编码是否需要转换。
func sniffMP4(path string) (mp4Info, error) {
	info := mp4Info{Container: ContainerMP4}

	file, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return info, err
	}

	if err := parseMP4Boxes(file, 0, stat.Size(), 0, &info); err != nil {
		return info, nil // 结构损坏时返回已解析到的部分，避免影响播放
	}
	return info, nil
}

const maxBoxDepth = 8

// isCompleteMP4 判断 MP4 是否已经写入 moov。
//
// 转码被断电或强制结束中断时 moov 尚未写入（+faststart 在编码结束才处理），
// 这类文件必须视为无效缓存，否则会被误当作可播放结果。
func isCompleteMP4(path string) bool {
	info, err := sniffMP4(path)
	return err == nil && info.HasMoov
}

var containerBoxes = map[string]struct{}{
	"moov": {},
	"trak": {},
	"mdia": {},
	"minf": {},
	"stbl": {},
}

func parseMP4Boxes(r io.ReaderAt, start, end int64, depth int, info *mp4Info) error {
	if depth > maxBoxDepth {
		return errors.New("mp4: box nesting too deep")
	}

	offset := start
	header := make([]byte, 8)

	for offset+8 <= end {
		if _, err := r.ReadAt(header, offset); err != nil {
			return err
		}

		size := int64(binary.BigEndian.Uint32(header[:4]))
		boxType := string(header[4:8])
		headerSize := int64(8)

		switch size {
		case 0:
			size = end - offset
		case 1:
			large := make([]byte, 8)
			if _, err := r.ReadAt(large, offset+8); err != nil {
				return err
			}
			size = int64(binary.BigEndian.Uint64(large))
			headerSize = 16
		}

		if size < headerSize || offset+size > end {
			return nil
		}

		switch boxType {
		case "ftyp":
			brand := make([]byte, 4)
			if _, err := r.ReadAt(brand, offset+headerSize); err == nil {
				info.Container = containerFromBrand(string(brand), info.Container)
			}
		case "stsd":
			readSampleEntries(r, offset+headerSize, offset+size, info)
		default:
			if _, ok := containerBoxes[boxType]; ok {
				if boxType == "moov" {
					info.HasMoov = true
				}
				_ = parseMP4Boxes(r, offset+headerSize, offset+size, depth+1, info)
			}
		}

		offset += size
	}
	return nil
}

func containerFromBrand(brand string, fallback Container) Container {
	switch brand {
	case "qt  ":
		return ContainerMOV
	case "M4V ", "M4VP", "M4VH":
		return ContainerM4V
	case "3gp4", "3gp5", "3gp6", "3gp7", "3g2a":
		return ContainerThreeGP
	default:
		return fallback
	}
}

// readSampleEntries 解析 stsd 中的 sample entry，提取编码 fourcc。
//
// stsd 布局：version/flags(4) + entry_count(4) + 若干 entry(size(4) + fourcc(4) + ...)
func readSampleEntries(r io.ReaderAt, start, end int64, info *mp4Info) {
	buf := make([]byte, 8)
	if start+8 > end {
		return
	}
	if _, err := r.ReadAt(buf, start); err != nil {
		return
	}

	count := int(binary.BigEndian.Uint32(buf[4:8]))
	offset := start + 8

	for i := 0; i < count && offset+8 <= end; i++ {
		if _, err := r.ReadAt(buf, offset); err != nil {
			return
		}
		size := int64(binary.BigEndian.Uint32(buf[:4]))
		codec := codecFromFourCC(string(buf[4:8]))
		if size < 8 || offset+size > end {
			return
		}

		switch {
		case codec.IsVideo():
			info.VideoCodecs = append(info.VideoCodecs, codec)
		case codec.IsAudio():
			info.AudioCodecs = append(info.AudioCodecs, codec)
		}
		offset += size
	}
}

// primaryVideoCodec 返回第一个可识别的主视频编码。
func (i mp4Info) primaryVideoCodec() Codec {
	if len(i.VideoCodecs) == 0 {
		return CodecUnknown
	}
	return i.VideoCodecs[0]
}

// primaryAudioCodec 返回第一个可识别的主音频编码。
func (i mp4Info) primaryAudioCodec() Codec {
	if len(i.AudioCodecs) == 0 {
		return CodecUnknown
	}
	return i.AudioCodecs[0]
}
