package service

import (
	"errors"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/filetype"
	"github.com/bing/bing-play/internal/torrentmgr"
	"github.com/wailsapp/wails/v3/pkg/application"
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
	resourceDao    *dao.ResourceDAO
	torrentManager *torrentmgr.TorrentManager
}

// NewVideoService 创建 VideoService
func NewVideoService(
	resourceDao *dao.ResourceDAO,
	torrentManager *torrentmgr.TorrentManager,
) *VideoService {
	return &VideoService{
		resourceDao:    resourceDao,
		torrentManager: torrentManager,
	}
}

// 名称中的分隔符，用于匹配紧跟在剧名后的集数标记
const nameSeparators = `[._\s\-\[\]()（）【】]+`

// episodeMarkers 常见的集数标记：第01集、01话、S01E01、EP01、E01、0x01、[01]、纯数字
var episodeMarkers = []*regexp.Regexp{
	regexp.MustCompile(`第\s*[0-9零〇一二三四五六七八九十百千万两]+\s*[集话期回]`),
	regexp.MustCompile(`[0-9零〇一二三四五六七八九十百千万两]+\s*[集话期回]`),
	regexp.MustCompile(`(?i)(?:^|` + nameSeparators + `)s\d{1,2}[._\s\-]*e\d{1,4}`),
	regexp.MustCompile(`(?i)(?:^|` + nameSeparators + `)(?:episode|ep|e)[._\s\-]*\d{1,4}`),
	regexp.MustCompile(`(?i)(?:^|` + nameSeparators + `)0x\d{1,4}`),
	regexp.MustCompile(`(?i)(?:^|` + nameSeparators + `)s\d{1,2}\s*$`),
	regexp.MustCompile(`[\[\(（【]\s*\d{1,3}\s*[\]\)）】]`),
	regexp.MustCompile(`^\d{1,3}$`),
}

// 末尾的单独数字，如 "Show - 01" 中的 01
var trailingEpisodeNumber = regexp.MustCompile(`[._\s-]+(\d{1,3})\s*$`)

// 季、部、卷等编号，末尾数字属于这些编号时不应当作集数删除
var volumeNumber = regexp.MustCompile(`(?i)(?:season|part|volume|vol|disc|cd)[._\s]*$`)

// seriesGroupName 去掉名称中的集数标记，只保留前边的剧名。
// 例如 "Show.S01E01.1080p" 得到 "Show"，"Show Season 1" 保持原样。
// 集数标记前没有内容时返回空字符串。
func seriesGroupName(name string) string {
	name = strings.TrimSpace(name)
	cut := len(name)

	for _, marker := range episodeMarkers {
		if loc := marker.FindStringIndex(name); loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}

	// 末尾数字默认视为集数，但 "Season 1"、"Part 2" 这类季/部编号要保留
	if loc := trailingEpisodeNumber.FindStringSubmatchIndex(name); loc != nil && loc[0] < cut {
		if !volumeNumber.MatchString(name[:loc[2]]) {
			cut = loc[0]
		}
	}

	if cut == 0 {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(name[:cut]), " \t._-—·[]()（）【】")
}

// groupNameForLocalVideo 从文件名中取出剧名作为分组名。
// 文件名只有集数（如 Show/01.mkv）时，退回所在目录名。
func groupNameForLocalVideo(path, fileName string) string {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if name := seriesGroupName(base); name != "" {
		return name
	}

	dir := filepath.Base(filepath.Dir(path))
	if dir != "." && dir != string(filepath.Separator) {
		if name := seriesGroupName(dir); name != "" {
			return name
		}
	}

	return base
}

// OpenVideoResult 打开视频后返回的资源信息。
type OpenVideoResult struct {
	ID       int64  `json:"id"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	MimeType string `json:"mimeType"`
}

// PlaylistItem 播放列表条目，ResourceName 用于前端分组。
type PlaylistItem struct {
	ID           int64  `json:"id"`
	ResourceName string `json:"resourceName"`
	FileName     string `json:"fileName"`
}

// ListPlaylist 返回左侧播放列表所需的本地资源。
func (s *VideoService) ListPlaylist() ([]PlaylistItem, error) {
	resources, err := s.resourceDao.ListPlayable()
	if err != nil {
		return nil, err
	}

	items := make([]PlaylistItem, 0, len(resources))
	for _, resource := range resources {
		item := PlaylistItem{ID: resource.ID}
		if resource.ResourceName != nil {
			item.ResourceName = *resource.ResourceName
		}
		if resource.FileName != nil {
			item.FileName = *resource.FileName
		}
		items = append(items, item)
	}

	return items, nil
}

// RemovePlaylistItems 从播放列表批量移除资源。
//
// 本地拖入的视频只删除数据库记录，不删除磁盘文件；
// 种子资源重置为「未下载」，同样保留磁盘文件。单个失败不会中断其余条目。
func (s *VideoService) RemovePlaylistItems(ids []int64) error {
	return s.torrentManager.RemoveFromPlaylist(ids)
}

// NewVideoWailsService 把本地视频服务暴露给前端，播放列表通过 Wails binding 获取。
func NewVideoWailsService(s *VideoService) ServiceOut {
	return ServiceOut{
		Service: application.NewService(s),
	}
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

	fileName := filepath.Base(path)
	fileSize := info.Size()
	mimeType := mime.TypeByExtension(filepath.Ext(path))
	fileDir := filepath.Dir(path)

	// local_path 统一保存文件所在目录：本地视频与已下载种子共用同一套匹配规则，
	// 再次移入已下载的视频时直接复用原记录，播放列表不会出现重复条目。
	resource, err := s.resourceDao.FindByLocalFile(fileDir, fileName)
	if err != nil {
		slog.Error("查询本地资源失败", "path", path, "err", err)
		return nil, ErrVideoOpenFailed
	}

	now := time.Now()

	// 命中已下载种子记录时直接复用，不改变其类型与下载状态，避免干扰做种/续传。
	if resource != nil && resource.Kind == models.KindTorrent {
		return &OpenVideoResult{
			ID:       resource.ID,
			FileName: fileName,
			FileSize: fileSize,
			MimeType: mimeType,
		}, nil
	}

	if resource == nil {
		resource = &models.Resource{
			CreateTime: &now,
		}
	}
	resource.LocalPath = fileDir

	// 本地视频没有种子名称，从文件名中取出剧名作为播放列表分组名，
	// 去掉集数部分，让同一部剧的不同集聚到一组。
	if resource.ResourceName == nil || *resource.ResourceName == "" {
		groupName := groupNameForLocalVideo(path, fileName)
		resource.ResourceName = &groupName
	}

	resource.FileName = &fileName
	resource.FileSize = &fileSize
	resource.MimeType = &mimeType
	resource.ResourceType = filetype.Video
	resource.LocalState = models.LocalPresent
	resource.LocalLossReason = models.LossNone
	resource.VerifiedAt = &now
	resource.LocalCheckedAt = &now
	resource.UpdateTime = &now
	if resource.CreateTime == nil {
		resource.CreateTime = &now
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
