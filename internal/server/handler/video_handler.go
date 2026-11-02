package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/bing/bing-play/internal/db/dao"
	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/i18n"
	"github.com/bing/bing-play/internal/logger"
	"github.com/bing/bing-play/internal/server"
	"github.com/bing/bing-play/internal/service"
	"github.com/bing/bing-play/internal/torrentmgr"
	"github.com/bing/bing-play/internal/transcode"

	"github.com/anacrolix/torrent"
	"go.uber.org/fx"
)

type VideoHandler struct {
	resourceDao    *dao.ResourceDAO
	videoService   *service.VideoService
	i18nService    *i18n.I18nService
	torrentManager *torrentmgr.TorrentManager
	transcoder     *transcode.Scheduler
}

type VideoHandlerParams struct {
	fx.In
	ResourceDao    *dao.ResourceDAO
	VideoService   *service.VideoService
	I18n           *i18n.I18nService
	TorrentManager *torrentmgr.TorrentManager
	Transcoder     *transcode.Scheduler
}

func NewVideoHandler(params VideoHandlerParams) *VideoHandler {

	return &VideoHandler{
		resourceDao:    params.ResourceDao,
		videoService:   params.VideoService,
		i18nService:    params.I18n,
		torrentManager: params.TorrentManager,
		transcoder:     params.Transcoder,
	}
}

func VideoRoute(h *VideoHandler) server.RouteOut {

	return server.RouteOut{
		Handler: h,
	}
}

func (h *VideoHandler) RegisterRoutes(mux *http.ServeMux) {

	mux.HandleFunc("/video/open", h.OpenLocal)
	mux.HandleFunc("/video/prepare/{id}", h.Prepare)
	mux.HandleFunc("/video/transcode/{jobId}", h.TranscodeStatus)
	mux.HandleFunc("/video/cache/{name}", h.VideoCache)
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

// prepareVideoResponse /video/prepare 与 /video/transcode/{jobId} 的返回结构。
//
// URL 为空表示由前端继续使用原视频地址；Message 是后端按当前语言生成好的提示文案。
type prepareVideoResponse struct {
	ID       int64            `json:"id,omitempty"`
	URL      string           `json:"url,omitempty"`
	Action   transcode.Action `json:"action"`
	JobID    string           `json:"jobId,omitempty"`
	Phase    transcode.Phase  `json:"phase,omitempty"`
	Progress float64          `json:"progress"`
	Message  string           `json:"message,omitempty"`
}

// 转码缓存文件名：调度器用 sha256 前 24 位加 .mp4 命名，只允许这类文件被读取。
var transcodeCacheNamePattern = regexp.MustCompile(`^[a-f0-9]{24}\.mp4$`)

// Prepare 返回播放地址前先探测格式并按需异步转码。
//
// 返回体里的 action 决定前端的后续动作：
//   - play_direct / fallback / defer：直接播放 url（fallback 时附带 message）；
//   - convert：携带 jobId，前端轮询 /video/transcode/{jobId}；
//   - play_converted：播放转换后的缓存地址。
func (h *VideoHandler) Prepare(w http.ResponseWriter, r *http.Request) {
	h.allowCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, h.i18nService.T("http.error_method_not_allowed", nil), http.StatusMethodNotAllowed)
		return
	}

	resource, err := h.resourceDao.GetLocalFile(r.PathValue("id"))
	if err != nil {
		http.Error(w, h.i18nService.T("video.error_query_failed", nil), http.StatusInternalServerError)
		return
	}
	if resource == nil {
		http.Error(w, h.i18nService.T("video.error_not_found", nil), http.StatusNotFound)
		return
	}

	direct := prepareVideoResponse{
		ID:     resource.ID,
		URL:    videoURL(r, resource.ID),
		Action: transcode.ActionPlayDirect,
	}

	// 边下边播的种子任务还没有完整本地文件，无法转码，直接播放原地址。
	if !localPlayable(resource) {
		h.writePrepare(w, direct)
		return
	}

	filePath, errKey, status := localFilePath(resource)
	if errKey != "" {
		http.Error(w, h.i18nService.T(errKey, nil), status)
		return
	}

	if h.transcoder == nil || !h.transcoder.Binaries().Available() {
		direct.Action = transcode.ActionFallback
		direct.Message = h.transcodeMessage(transcode.ReasonFFmpegMissing)
		h.writePrepare(w, direct)
		return
	}

	decision, _, err := h.transcoder.Plan(r.Context(), transcode.Request{Path: filePath})
	if err != nil {
		// 探测失败不影响播放：回退原文件，由播放器自行决定。
		direct.Action = transcode.ActionFallback
		direct.Message = h.transcodeMessage(transcode.ReasonProbeFailed)
		h.writePrepare(w, direct)
		return
	}

	switch decision.Action {
	case transcode.ActionConvert:
		resp := h.jobResponse(r, h.transcoder.Submit(transcode.Request{Path: filePath}), resource.ID)
		if resp.Phase != transcode.PhaseDone && resp.Phase != transcode.PhaseCanceled {
			// 立即提示“正在转换”，避免整个转换过程都显示笼统的准备中
			resp.Message = h.transcodeReasonMessage(decision.Reason)
		}
		h.writePrepare(w, resp)
	case transcode.ActionPlayDirect:
		h.writePrepare(w, direct)
	default:
		direct.Action = decision.Action
		direct.Message = h.transcodeMessage(decision.Reason)
		h.writePrepare(w, direct)
	}
}

// TranscodeStatus 返回转换任务进度快照，供前端轮询。
func (h *VideoHandler) TranscodeStatus(w http.ResponseWriter, r *http.Request) {
	h.allowCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, h.i18nService.T("http.error_method_not_allowed", nil), http.StatusMethodNotAllowed)
		return
	}
	if h.transcoder == nil {
		http.Error(w, h.i18nService.T("video.error_service_unavailable", nil), http.StatusServiceUnavailable)
		return
	}

	job, ok := h.transcoder.JobByID(r.PathValue("jobId"))
	if !ok {
		http.Error(w, h.i18nService.T("video.transcode.error.not_found", nil), http.StatusNotFound)
		return
	}
	h.writePrepare(w, h.jobResponse(r, job, 0))
}

// VideoCache 输出转码缓存文件，支持 Range 以便拖动进度条。
func (h *VideoHandler) VideoCache(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !transcodeCacheNamePattern.MatchString(name) {
		http.Error(w, h.i18nService.T("video.error_not_found", nil), http.StatusNotFound)
		return
	}
	if h.transcoder == nil {
		http.Error(w, h.i18nService.T("video.error_service_unavailable", nil), http.StatusServiceUnavailable)
		return
	}

	filePath := filepath.Join(h.transcoder.CacheDir(), name)
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, h.i18nService.T("video.error_not_found", nil), http.StatusNotFound)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, h.i18nService.T("video.error_stat_failed", nil), http.StatusInternalServerError)
		return
	}

	http.ServeContent(w, r, name, info.ModTime(), file)
}

// jobResponse 把任务快照转换为前端可直接使用的播放准备结果。
func (h *VideoHandler) jobResponse(r *http.Request, job *transcode.Job, id int64) prepareVideoResponse {
	snapshot := job.Snapshot()
	resp := prepareVideoResponse{
		ID:       id,
		Action:   transcode.ActionConvert,
		JobID:    job.ID,
		Phase:    snapshot.Phase,
		Progress: snapshot.Progress,
	}
	if snapshot.Phase != transcode.PhaseDone && snapshot.Phase != transcode.PhaseCanceled {
		if snapshot.Phase == transcode.PhaseConverting {
			resp.Message = h.transcodeReasonMessage(snapshot.Reason)
		}
		return resp
	}

	result, _ := job.Result()
	resp.Action = result.Action

	if result.Action == transcode.ActionPlayConverted && result.PlayPath != "" {
		resp.URL = cacheFileURL(r, result.PlayPath)
		return resp
	}

	resp.URL = videoURL(r, id)
	if result.Err != nil {
		resp.Message = h.transcodeMessage(result.Reason)
	}
	return resp
}

func (h *VideoHandler) writePrepare(w http.ResponseWriter, resp prepareVideoResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *VideoHandler) allowCORS(w http.ResponseWriter) {
	// 浏览器调试模式（vite）与 127.0.0.1:9090 属于跨域请求
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// transcodeMessage 把转换决策原因映射为当前语言的提示文案。
func (h *VideoHandler) transcodeMessage(reason transcode.Reason) string {
	return h.i18nService.T(transcodeReasonKey(reason), nil)
}

// transcodeReasonMessage 返回转换中的提示：能识别原因时说明具体原因，否则用通用文案。
func (h *VideoHandler) transcodeReasonMessage(reason transcode.Reason) string {
	if reason == "" || reason == transcode.ReasonCompatible {
		return h.i18nService.T("video.transcode.converting", nil)
	}
	return h.transcodeMessage(reason)
}

func transcodeReasonKey(reason transcode.Reason) string {
	switch reason {
	case transcode.ReasonHEVCDecoderMissing:
		return "video.transcode.reason.hevc_decoder_missing"
	case transcode.ReasonContainerUnsupported:
		return "video.transcode.reason.container_unsupported"
	case transcode.ReasonVideoUnsupported:
		return "video.transcode.reason.video_codec_unsupported"
	case transcode.ReasonAudioUnsupported:
		return "video.transcode.reason.audio_codec_unsupported"
	case transcode.ReasonHighBitDepth:
		return "video.transcode.reason.high_bit_depth"
	case transcode.ReasonFFmpegMissing:
		return "video.transcode.error.ffmpeg_missing"
	case transcode.ReasonInsufficientDisk:
		return "video.transcode.error.insufficient_disk"
	case transcode.ReasonMeteredNetwork:
		return "video.transcode.error.metered_network"
	case transcode.ReasonProbeFailed:
		return "video.transcode.error.probe_failed"
	case transcode.ReasonCanceled:
		return "video.transcode.error.canceled"
	case transcode.ReasonClosed:
		return "video.transcode.error.closed"
	default:
		return "video.transcode.error.convert_failed"
	}
}

// localPlayable 本地视频或已校验完成的种子资源可以读取本地文件。
func localPlayable(resource *models.Resource) bool {
	return resource.Kind == models.KindLocal ||
		(resource.TaskState == models.TaskCompleted && resource.LocalState == models.LocalPresent)
}

// localFilePath 解析资源对应的本地文件。
// local_path 统一保存文件所在目录，file_name 保存文件名（可带子目录）。
// 返回空 errKey 表示解析成功。
func localFilePath(resource *models.Resource) (string, string, int) {
	if resource.LocalPath == "" {
		return "", "video.error_empty_path", http.StatusNotFound
	}

	if resource.FileName == nil || *resource.FileName == "" {
		return "", "video.error_file_required", http.StatusBadRequest
	}

	filePath := filepath.Join(resource.LocalPath, *resource.FileName)
	if _, err := os.Stat(filePath); err != nil {
		return "", "video.error_not_found", http.StatusNotFound
	}
	return filePath, "", 0
}

func videoURL(r *http.Request, id int64) string {
	return fmt.Sprintf("http://%s/video/%d", r.Host, id)
}

func cacheFileURL(r *http.Request, path string) string {
	return fmt.Sprintf("http://%s/video/cache/%s", r.Host, filepath.Base(path))
}

func (h *VideoHandler) VideoLocal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	fmt.Println("视频id-----" + id)
	//todo 视频查询，判断是否下载完成等
	rr, err := h.resourceDao.GetLocalFile("1")
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

	resource, err := h.resourceDao.GetLocalFile(id)
	if err != nil {
		http.Error(w, h.i18nService.T("video.error_query_failed", nil), http.StatusInternalServerError)
		return
	}

	if resource == nil {
		http.Error(w, h.i18nService.T("video.error_not_found", nil), http.StatusNotFound)
		return
	}

	logger.App.Info("Video", "id", id, "taskState", resource.TaskState.String(), "localState", resource.LocalState.String())

	// 本地文件齐全（本地视频或已校验完成的种子）直接读本地文件。
	if resource.Kind == models.KindLocal || (resource.TaskState == models.TaskCompleted && resource.LocalState == models.LocalPresent) {
		h.videoLocal(w, r, resource)
		return
	}
	if resource.TaskState == models.TaskCompleted {
		// 已完成但本地文件缺失 / 损坏，明确报错而不是回退到种子。
		http.Error(w, h.i18nService.T("video.error_not_found", nil), http.StatusNotFound)
		return
	}
	if resource.TaskState == models.TaskPaused {
		http.Error(w, h.i18nService.T("video.error_task_paused", nil), http.StatusServiceUnavailable)
		return
	}
	if !resource.TaskState.Active() {
		http.Error(w, h.i18nService.T("download.error.task_not_found", nil), http.StatusNotFound)
		return
	}
	h.videoTorrent(w, r, resource)
}

func (h *VideoHandler) videoLocal(
	w http.ResponseWriter,
	r *http.Request,
	resource *models.Resource,
) {
	if resource.LocalPath == "" {
		http.Error(w, h.i18nService.T("video.error_empty_path", nil), http.StatusNotFound)
		return
	}

	fileName := r.URL.Query().Get("file")
	if fileName == "" && resource.FileName != nil {
		fileName = *resource.FileName
	}
	if fileName == "" {
		http.Error(w, h.i18nService.T("video.error_file_required", nil), http.StatusBadRequest)
		return
	}

	// local_path 统一保存文件所在目录，直接拼接文件名即可。
	filePath := filepath.Join(resource.LocalPath, fileName)
	if _, err := os.Stat(filePath); err != nil {
		http.Error(w, h.i18nService.T("video.error_not_found", nil), http.StatusNotFound)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, h.i18nService.T("video.error_open_failed", nil), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, h.i18nService.T("video.error_stat_failed", nil), http.StatusInternalServerError)
		return
	}

	name := filepath.Base(filePath)

	// 让 Go 原生 HTTP Server 处理 Range / 206 / Content-Length / Seek
	http.ServeContent(w, r, name, info.ModTime(), file)
}
func (h *VideoHandler) videoTorrent(
	w http.ResponseWriter,
	r *http.Request,
	resource *models.Resource,
) {
	if h.torrentManager == nil {
		http.Error(w, h.i18nService.T("video.error_service_unavailable", nil), http.StatusServiceUnavailable)
		return
	}
	// 根据 resource 找到正在下载的 TorrentJob
	job, err := h.torrentManager.GetJob(resource.InfoHash)
	if err != nil {
		http.Error(w, h.i18nService.T("download.error.task_not_found", nil), http.StatusNotFound)
		return
	}
	if job.CurrentState() == models.TaskPaused {
		http.Error(w, h.i18nService.T("video.error_task_paused", nil), http.StatusServiceUnavailable)
		return
	}
	tt := job.GetTorrent()
	if tt == nil || tt.Info() == nil {
		http.Error(w, h.i18nService.T("video.error_metadata_not_ready", nil), http.StatusServiceUnavailable)
		return
	}

	// 优先使用前端指定的文件，未指定时回退到资源记录中的文件名
	fileName := r.URL.Query().Get("file")
	logger.App.Info("Video", "file", fileName)
	if fileName == "" && resource.FileName != nil {
		fileName = *resource.FileName
	}
	file := job.GetFile(fileName)
	if file == nil {
		http.Error(w, h.i18nService.T("video.error_no_playable_file", nil), http.StatusNotFound)
		return
	}

	// 边下边播：允许数据下载，并优先获取正在播放文件的分片
	tt.AllowDataDownload()
	tt.DownloadAll()
	file.SetPriority(torrent.PiecePriorityHigh)

	reader := file.NewReader()
	defer reader.Close()

	// 客户端断开或 Range 请求结束时，及时取消阻塞中的读取
	reader.SetContext(r.Context())
	// 预读 4MB，减少边下边播时的卡顿
	reader.SetReadahead(4 << 20)

	name := fileName
	if name == "" {
		name = filepath.Base(file.Path())
	}

	// Torrent Reader 支持 Seek，HTTP Range 对应的 Seek 会由 ServeContent 触发
	http.ServeContent(w, r, name, time.Time{}, reader)
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
