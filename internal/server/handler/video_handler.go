package handler

import (
	"changeme/internal/container"
	"encoding/json"
	"fmt"
	"net/http"
)

type VideoHandler struct {
	cn *container.Container
}

func NewVideoHandler(container *container.Container) *VideoHandler {
	return &VideoHandler{
		cn: container,
	}
}

func (h *VideoHandler) VideoLocal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	fmt.Println("视频id-----" + id)
	//todo 视频查询，判断是否下载完成等
	resourceDao := h.cn.ResourceDao
	rr, err := resourceDao.Get("1")
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

		list, _ := resourceDao.GetPeerStats()
		data2, _ := json.Marshal(list)
		fmt.Println("JSON:", string(data2))
	}
	// 本地视频真实路径
	filePath := `D:\新媒体工具\视频下载\芒果TV\胡同\1.mp4`
	// 支持断点续播
	http.ServeFile(w, r, filePath)
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
