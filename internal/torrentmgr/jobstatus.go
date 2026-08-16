package torrentmgr

type JobStatus string

const (
	StatusQueued      JobStatus = "downMateInfo"
	StatusQueued      JobStatus = "downMateInfoCompleted"
	StatusQueued      JobStatus = "queued"
	StatusDownloading JobStatus = "downloading"
	StatusPaused      JobStatus = "paused"
	StatusCompleted   JobStatus = "completed"
	StatusFailed      JobStatus = "failed"
)
