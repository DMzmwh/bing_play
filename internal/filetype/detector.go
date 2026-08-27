package filetype

import (
	"path/filepath"
	"strings"
)

// Detect 根据文件路径判断文件类型
func Detect(path string) Category {
	// 获取文件扩展名，并统一转小写
	ext := strings.ToLower(filepath.Ext(path))

	switch {
	case exists(videoExtensions, ext):
		return Video
	case exists(audioExtensions, ext):
		return Audio
	case exists(imageExtensions, ext):
		return Image
	case exists(archiveExtensions, ext):
		return Archive
	case exists(subtitleExtensions, ext):
		return Subtitle
	default:
		return Unknown
	}
}

// IsVideo 判断是否为视频文件
func IsVideo(path string) bool {
	return Detect(path) == Video
}

// DetectResourceType 根据多个文件判断资源类型
//
// 规则：
// 1. 多个文件中包含视频 -> Video
// 2. 单个文件 -> 返回文件自身类型
// 3. 都不是 -> Unknown
func DetectResourceType(paths []string) Category {
	if len(paths) == 0 {
		return Unknown
	}
	// 单文件，直接返回文件类型
	if len(paths) == 1 {
		return Detect(paths[0])
	}
	// 多文件，只要包含视频，就认为是视频资源
	for _, path := range paths {
		if IsVideo(path) {
			return Video
		}
	}
	// 没有视频，取第一个文件类型
	return Detect(paths[0])
}

// IsArchive 判断是否为压缩文件
func IsArchive(path string) bool {
	return Detect(path) == Archive
}

// exists 判断扩展名是否存在于规则集合中
func exists(set map[string]struct{}, ext string) bool {
	_, ok := set[ext]
	return ok
}
