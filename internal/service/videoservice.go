package service

import (
	"errors"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/filetype"
)

// 打开本地视频时可能返回的业务错误。
// 错误文本本身就是 i18n 消息 ID，调用方直接 T(err.Error()) 取当前语言文案。
var (
	ErrVideoPathEmpty   = errors.New("video.error_empty_path")
	ErrVideoNotFound    = errors.New("video.error_not_found")
	ErrVideoIsDir       = errors.New("video.error_is_dir")
	ErrVideoUnsupported = errors.New("video.error_unsupported")
	ErrVideoOpenFailed  = errors.New("video.error_open_failed")
)

// VideoService 本地视频资源服务。
//
// 负责：
//  1. 校验本地视频路径
//  2. 登记 / 复用本地资源记录
//  3. 返回播放所需的信息
//
// 不负责：
//  1. 视频流输出，仍由 HTTP 服务 /video/{id} 提供（浏览器只能通过 HTTP 拉流）
type VideoService struct {
	resourceDao *dao.ResourceDAO
}

// NewVideoService 创建 VideoService
func NewVideoService(resourceDao *dao.ResourceDAO) *VideoService {
	return &VideoService{
		resourceDao: resourceDao,
	}
}

// OpenVideoResult 打开视频后返回的资源信息。
type OpenVideoResult struct {
	ID       int64  `json:"id"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	MimeType string `json:"mimeType"`
}

// OpenLocalVideo 校验本地视频文件并登记为资源。
// 同一路径重复打开时复用已有资源记录，不会新增记录。
func (s *VideoService) OpenLocalVideo(path string) (*OpenVideoResult, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, ErrVideoPathEmpty
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, ErrVideoNotFound
	}
	if info.IsDir() {
		return nil, ErrVideoIsDir
	}
	if !filetype.IsVideo(path) {
		return nil, ErrVideoUnsupported
	}

	resource, err := s.resourceDao.GetByLocalPath(path)
	if err != nil {
		slog.Error("查询本地资源失败", "path", path, "err", err)
		return nil, ErrVideoOpenFailed
	}

	now := time.Now()
	fileName := filepath.Base(path)
	fileSize := info.Size()
	mimeType := mime.TypeByExtension(filepath.Ext(path))

	if resource == nil {
		resource = &models.Resource{
			LocalPath:     path,
			CreateTime:    &now,
			CompletedTime: &now,
		}
	}

	resource.FileName = &fileName
	resource.FileSize = &fileSize
	resource.MimeType = &mimeType
	resource.ResourceType = filetype.Video
	resource.LocalStatus = models.LocalStatusExists
	resource.DownloadStatus = models.JobStatusCompleted
	resource.UpdateTime = &now
	if resource.CompletedTime == nil {
		resource.CompletedTime = &now
	}

	if err := s.resourceDao.SaveLocalResource(resource); err != nil {
		slog.Error("保存本地资源失败", "path", path, "err", err)
		return nil, ErrVideoOpenFailed
	}

	return &OpenVideoResult{
		ID:       resource.ID,
		FileName: fileName,
		FileSize: fileSize,
		MimeType: mimeType,
	}, nil
}
