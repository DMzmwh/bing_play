package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/i18n"
	"github.com/bing/bing-play/internal/server"
	"github.com/bing/bing-play/internal/service"
	"github.com/bing/bing-play/internal/torrentmgr"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/fx"
)

type VideoHandler struct {
	resourceDao    *dao.ResourceDAO
	videoService   *service.VideoService
	i18nService    *i18n.I18nService
	torrentManager *torrentmgr.TorrentManager
}

type VideoHandlerParams struct {
	fx.In
	ResourceDao    *dao.ResourceDAO
	VideoService   *service.VideoService
	I18n           *i18n.I18nService
	TorrentManager *torrentmgr.TorrentManager
}

func NewVideoHandler(params VideoHandlerParams) *VideoHandler {

	return &VideoHandler{
		resourceDao:  params.ResourceDao,
		videoService: params.VideoService,
		i18nService:  params.I18n,
	}
}

func VideoRoute(h *VideoHandler) server.RouteOut {

	return server.RouteOut{
		Handler: h,
	}
}

func (h *VideoHandler) RegisterRoutes(mux *http.ServeMux) {

	mux.HandleFunc("/video/open", h.OpenLocal)
	mux.HandleFunc("/video/{id}", h.Video)
	mux.HandleFunc("/video/local/{id}", h.VideoLocal)
	mux.HandleFunc("/stream/{id}", h.Stream)

}

// openVideoRequest 前端拖拽视频后提交的请求体。
type openVideoRequest struct {
	Path string `json:"path"`
}

// openVideoResponse 后端返回的可播放资源信息。
type openVideoResponse struct {
	ID       int64  `json:"id"`
	URL      string `json:"url"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	MimeType string `json:"mimeType"`
}

// OpenLocal 接收本地视频地址并返回播放地址。
// 该接口是 VideoService 的 HTTP 适配层，业务逻辑（校验、登记、复用）都在 service。
func (h *VideoHandler) OpenLocal(w http.ResponseWriter, r *http.Request) {

	// 浏览器调试模式（vite）与 127.0.0.1:9090 属于跨域请求
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, h.i18nService.T("http.error_method_not_allowed", nil), http.StatusMethodNotAllowed)
		return
	}

	var req openVideoRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, h.i18nService.T("http.error_invalid_request", nil), http.StatusBadRequest)
		return
	}

	result, err := h.videoService.OpenLocalVideo(req.Path)
	if err != nil {
		h.writeOpenLocalError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(openVideoResponse{
		ID:       result.ID,
		URL:      fmt.Sprintf("http://%s/video/%d", r.Host, result.ID),
		FileName: result.FileName,
		FileSize: result.FileSize,
		MimeType: result.MimeType,
	})
}

// writeOpenLocalError 把业务错误映射为 HTTP 状态码，并按当前语言输出提示信息。
func (h *VideoHandler) writeOpenLocalError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, service.ErrVideoPathEmpty),
		errors.Is(err, service.ErrVideoIsDir),
		errors.Is(err, service.ErrVideoUnsupported):
		status = http.StatusBadRequest
	case errors.Is(err, service.ErrVideoNotFound):
		status = http.StatusNotFound
	}
	http.Error(w, h.i18nService.T(err.Error(), nil), status)
}

func (h *VideoHandler) VideoLocal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	fmt.Println("视频id-----" + id)
	//todo 视频查询，判断是否下载完成等
	rr, err := h.resourceDao.Get("1")
	if err != nil {
		panic(err)
	}
	data2, _ := json.Marshal(err)
	fmt.Println("JSON:", string(data2))
	fmt.Println("---------------1")
	if rr != nil {
		fmt.Println(rr.FileName)
		data, _ := json.Marshal(rr)
		fmt.Println("JSON:", string(data))
		fmt.Println("---------------2")

		//list, _ := resourceDao.GetPeerStats()
		//data2, _ := json.Marshal(list)
		fmt.Println("JSON:", string(data2))
	}
	// 本地视频真实路径
	filePath := `D:\新媒体工具\视频下载\芒果TV\胡同\1.mp4`
	// 支持断点续播
	http.ServeFile(w, r, filePath)
}

func (h *VideoHandler) Video(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	resource, err := h.resourceDao.Get(id)
	if err != nil {
		http.Error(w, "查询失败", http.StatusInternalServerError)
		return
	}

	if resource == nil {
		http.NotFound(w, r)
		return
	}

	if resource.DownloadStatus == models.JobStatusCompleted {
		h.videoLocal(w, r, resource)
		return
	}

	h.videoTorrent(w, r, resource)
}

func (h *VideoHandler) videoLocal(
	w http.ResponseWriter,
	r *http.Request,
	resource *models.Resource,
) {
	filePath := resource.LocalPath
	infoV, err := os.Stat(filePath)
	if err != nil {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}
	if infoV.IsDir() {
		fileItemPath := r.URL.Query().Get("file")
		if fileItemPath != "" {
			filePath = filepath.Join(filePath, fileItemPath)
		} else {
			http.Error(w, "请选择文件", http.StatusInternalServerError)
			return
		}
	}
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "打开视频失败", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, "获取视频信息失败", http.StatusInternalServerError)
		return
	}

	// 让 Go 原生 HTTP Server 处理：
	// Range
	// 206 Partial Content
	// Content-Length
	// Content-Range
	// Seek
	http.ServeContent(
		w,
		r,
		*resource.FileName,
		info.ModTime(),
		file,
	)
}
func (h *VideoHandler) videoTorrent(
	w http.ResponseWriter,
	r *http.Request,
	resource *models.Resource,
) {
	//根据 resource 找到正在下载的 TorrentJob
	job, error := h.torrentManager.GetJob(resource.InfoHash)
	if error != nil {
		http.Error(w, error.Error(), http.StatusNotFound)
		return
	}
	//
	//t := job.GetTorrent()
	//
	//if t == nil {
	//	http.Error(w, "Torrent 未准备好", http.StatusServiceUnavailable)
	//	return
	//}

	filePath := r.URL.Query().Get("file")
	// 找到视频文件
	file := job.GetFile(filePath)
	//file := findTorrentFile(t, resource.FileName)
	if file == nil {
		http.Error(w, "Torrent 文件不存在", http.StatusNotFound)
		return
	}

	reader := file.NewReader()
	defer reader.Close()

	// Torrent Reader 支持 Seek
	// HTTP Range 对应的 Seek 会由下面的 ServeContent 触发
	http.ServeContent(
		w,
		r,
		*resource.FileName,
		time.Time{},
		reader,
	)
}
func (h *VideoHandler) Stream(w http.ResponseWriter, r *http.Request) {
	//
	//id := strings.TrimPrefix(
	//	r.URL.Path,
	//	"/video/",
	//)
	//reader, err := h.torrent.GetFileReader(id)
	//if err != nil {
	//	http.Error(
	//		w,
	//		err.Error(),
	//		500,
	//	)
	//	return
	//}
	//http.ServeContent(
	//	w,
	//	r,
	//	"movie.mkv",
	//	time.Now(),
	//	reader,
	//)
}
