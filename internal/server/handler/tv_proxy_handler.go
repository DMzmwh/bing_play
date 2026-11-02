package handler

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/bing/bing-play/internal/server"
)

const (
	// tvProxyMaxPlaylistSize 单个 HLS 播放列表大小上限，避免异常响应占用过多内存。
	tvProxyMaxPlaylistSize = 8 << 20
	// tvProxyUserAgent 部分 IPTV 源会按 UA 拒绝请求，使用常见桌面浏览器 UA。
	tvProxyUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// playlistURIPattern 匹配 HLS 标签中的 URI 属性，如 #EXT-X-KEY 的密钥地址。
var playlistURIPattern = regexp.MustCompile(`URI="([^"]*)"`)

// TVProxyHandler 电视直播流的本地代理。
//
// 前端直连失败（CORS、403、超时等）后把播放地址切换到该代理：
// 播放列表中的分片、子列表、密钥地址会被重写为代理地址，
// 由后端携带 UA/Referer 拉取并透传，规避浏览器跨域与防盗链限制。
type TVProxyHandler struct {
	client *http.Client
}

// NewTVProxyHandler 创建代理 handler。
func NewTVProxyHandler() *TVProxyHandler {
	return &TVProxyHandler{
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// TVProxyRoute 把代理 handler 注册到本地 HTTP 服务的路由组。
func TVProxyRoute(h *TVProxyHandler) server.RouteOut {
	return server.RouteOut{
		Handler: h,
	}
}

// RegisterRoutes 注册代理路由。
func (h *TVProxyHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/tv/proxy", h.Proxy)
}

// Proxy 拉取订阅源地址并转发；src 为经过 URL 编码的原始地址。
func (h *TVProxyHandler) Proxy(w http.ResponseWriter, r *http.Request) {
	// hls.js 通过 fetch 请求播放列表与分片，代理响应必须允许跨域。
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	target, err := url.Parse(r.URL.Query().Get("src"))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		http.Error(w, "invalid src", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(w, "invalid src", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", tvProxyUserAgent)
	req.Header.Set("Accept", "*/*")
	// 不少直播源校验 Referer，使用源站根地址作为来源。
	req.Header.Set("Referer", target.Scheme+"://"+target.Host+"/")
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		w.WriteHeader(resp.StatusCode)
		return
	}

	if isHLSPlaylist(resp.Header.Get("Content-Type"), resp.Request.URL.Path) {
		body, err := io.ReadAll(io.LimitReader(resp.Body, tvProxyMaxPlaylistSize))
		if err != nil {
			http.Error(w, "read playlist failed", http.StatusBadGateway)
			return
		}
		rewritten := rewritePlaylist(body, resp.Request.URL)

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		// 直播列表持续更新，禁止缓存。
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(rewritten)
		}
		return
	}

	for _, key := range []string{
		"Content-Type", "Content-Length", "Content-Range",
		"Accept-Ranges", "Last-Modified", "ETag",
	} {
		if value := resp.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
	}
}

// isHLSPlaylist 判断上游响应是否为 HLS 播放列表。
func isHLSPlaylist(contentType, path string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "m3u8") {
		return true
	}
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".m3u8") || strings.HasSuffix(lower, ".m3u")
}

// rewritePlaylist 把播放列表中的所有地址改写到本地代理。
func rewritePlaylist(body []byte, base *url.URL) []byte {
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			upper := strings.ToUpper(trimmed)
			switch {
			case strings.HasPrefix(upper, "#EXT-X-KEY:"),
				strings.HasPrefix(upper, "#EXT-X-MAP:"),
				strings.HasPrefix(upper, "#EXT-X-MEDIA:"),
				strings.HasPrefix(upper, "#EXT-X-I-FRAME-STREAM-INF:"):
				lines[i] = playlistURIPattern.ReplaceAllStringFunc(line, func(match string) string {
					value := strings.TrimSuffix(strings.TrimPrefix(match, `URI="`), `"`)
					return `URI="` + proxyRef(base, value) + `"`
				})
			}
			continue
		}

		// 普通行是分片或子播放列表地址（相对于当前播放列表解析）。
		lines[i] = proxyRef(base, trimmed)
	}
	return []byte(strings.Join(lines, "\n"))
}

// proxyRef 把相对/绝对地址转换为本地代理地址。
func proxyRef(base *url.URL, raw string) string {
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	abs := base.ResolveReference(ref)
	return "/tv/proxy?src=" + url.QueryEscape(abs.String())
}
