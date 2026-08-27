package filetype

// 视频文件扩展名
var videoExtensions = map[string]struct{}{
	// 常见视频格式
	".mp4":  {},
	".m4v":  {},
	".mkv":  {},
	".avi":  {},
	".mov":  {},
	".wmv":  {},
	".flv":  {},
	".webm": {},

	// MPEG格式
	".mpeg": {},
	".mpg":  {},
	".mpe":  {},
	".m1v":  {},
	".m2v":  {},
	".mpv":  {},

	// 传输流格式
	".ts":   {},
	".mts":  {},
	".m2ts": {},

	// 移动设备格式
	".3gp": {},
	".3g2": {},

	// RealMedia格式
	".rm":   {},
	".rmvb": {},

	// 其他视频格式
	".qt":   {},
	".f4v":  {},
	".swf":  {},
	".ogv":  {},
	".mk3d": {},
	".mka":  {},
	".vob":  {},
	".ifo":  {},
	".dv":   {},
	".asf":  {},
}

// 音频文件扩展名
var audioExtensions = map[string]struct{}{
	".mp3":  {},
	".flac": {},
	".wav":  {},
	".aac":  {},
	".ogg":  {},
	".m4a":  {},
	".wma":  {},
}

// 图片文件扩展名
var imageExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".gif":  {},
	".webp": {},
	".bmp":  {},
}

// 压缩文件扩展名
var archiveExtensions = map[string]struct{}{
	".zip": {},
	".rar": {},
	".7z":  {},
	".tar": {},
	".gz":  {},
	".bz2": {},
	".xz":  {},
}

// 字幕文件扩展名
var subtitleExtensions = map[string]struct{}{
	".srt": {},
	".ass": {},
	".ssa": {},
	".sub": {},
	".vtt": {},
}
