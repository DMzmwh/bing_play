package handler

import (
	"encoding/json"
	"fmt"
	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/server"
	"github.com/bing/bing-play/internal/torrentmgr"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/fx"
)

type VideoHandler struct {
	resourceDao    *dao.ResourceDAO
	torrentManager *torrentmgr.TorrentManager
}

type VideoHandlerParams struct {
	fx.In
	ResourceDao    *dao.ResourceDAO
	TorrentManager *torrentmgr.TorrentManager
}

func NewVideoHandler(params VideoHandlerParams) *VideoHandler {

	return &VideoHandler{
		resourceDao: params.ResourceDao,
	}
}

func VideoRoute(h *VideoHandler) server.RouteOut {

	return server.RouteOut{
		Handler: h,
	}
}

func (h *VideoHandler) RegisterRoutes(mux *http.ServeMux) {

	mux.HandleFunc("/video/{id}", h.Video)
	mux.HandleFunc("/video/local/{id}", h.VideoLocal)
	mux.HandleFunc("/stream/{id}", h.Stream)

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
