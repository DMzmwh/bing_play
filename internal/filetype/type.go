package filetype

// Category 文件类型分类
type Category string

const (
	// Unknown 未知类型
	Unknown Category = "unknown"
	// Video 视频文件
	Video Category = "video"
	// Audio 音频文件
	Audio Category = "audio"
	// Image 图片文件
	Image Category = "image"
	// Archive 压缩文件
	Archive Category = "archive"
	// Subtitle 字幕文件
	Subtitle Category = "subtitle"
	// Document 文档文件
	Document Category = "document"
)
