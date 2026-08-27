package filetype

// FileInfo 文件分析结果
type FileInfo struct {
	// 文件路径
	Path string `json:"path"`

	// 文件分类
	Category Category `json:"category"`

	// 是否可以直接播放
	Playable bool `json:"playable"`
}

// Analyze 分析文件类型
func Analyze(path string) FileInfo {
	category := Detect(path)

	return FileInfo{
		Path:     path,
		Category: category,
		// 当前只有视频类型支持播放器直接打开
		Playable: category == Video,
	}
}
