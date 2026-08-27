package filetype

var mimeTypes = map[string]string{

	// video
	".mp4":  "video/mp4",
	".mkv":  "video/x-matroska",
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".mov":  "video/quicktime",
	".flv":  "video/x-flv",
	".ts":   "video/mp2t",

	// audio
	".mp3":  "audio/mpeg",
	".flac": "audio/flac",
	".wav":  "audio/wav",

	// image
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
}
