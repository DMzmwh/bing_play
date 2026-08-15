package models

type Resource struct {
	ID string `gorm:"primaryKey"`

	PeerID string `gorm:"index"`

	FileID string

	FileName string

	FileSize int64

	Version int

	Timestamp int64
}

type PeerStat struct {
	PeerID string
	Count  int
}
