package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newTestProxy 启动一个注册了 /tv/proxy 的本地代理服务，返回其根地址。
func newTestProxy(t *testing.T) (string, func()) {
	t.Helper()

	h := NewTVProxyHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	return srv.URL, srv.Close
}

// proxySrc 生成代理地址，便于在断言中复用。
func proxySrc(base, raw string) string {
	return base + "/tv/proxy?src=" + url.QueryEscape(raw)
}

// proxyRef 生成播放列表里使用的相对代理地址。
func proxyRefPath(raw string) string {
	return "/tv/proxy?src=" + url.QueryEscape(raw)
}

func TestTVProxyRewritesPlaylistAndStreamsSegment(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, strings.Join([]string{
				"#EXTM3U",
				`#EXT-X-KEY:METHOD=AES-128,URI="key.bin"`,
				`#EXT-X-MAP:URI="init.mp4"`,
				"#EXT-X-STREAM-INF:BANDWIDTH=1000000",
				"chunk/index.m3u8",
			}, "\n"))
		case "/live/chunk/index.m3u8":
			w.Header().Set("Content-Type", "application/x-mpegURL")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:4.0,\nseg-1.ts\n")
		case "/live/chunk/seg-1.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = io.WriteString(w, "SEGMENT")
		case "/live/key.bin":
			_, _ = io.WriteString(w, "KEY")
		case "/live/init.mp4":
			_, _ = io.WriteString(w, "INIT")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	base, closeProxy := newTestProxy(t)
	defer closeProxy()

	playlistURL := proxySrc(base, upstream.URL+"/live/master.m3u8")
	resp, err := http.Get(playlistURL)
	if err != nil {
		t.Fatalf("请求代理播放列表失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS 头 = %q, want *", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取代理播放列表失败: %v", err)
	}
	content := string(body)

	wantKey := `URI="` + proxyRefPath(upstream.URL+"/live/key.bin") + `"`
	if !strings.Contains(content, wantKey) {
		t.Errorf("密钥地址未重写，播放列表:\n%s", content)
	}
	wantMap := `URI="` + proxyRefPath(upstream.URL+"/live/init.mp4") + `"`
	if !strings.Contains(content, wantMap) {
		t.Errorf("初始化分片地址未重写，播放列表:\n%s", content)
	}

	var variantPath string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			variantPath = line
		}
	}
	if variantPath == "" {
		t.Fatalf("未找到子播放列表地址，播放列表:\n%s", content)
	}

	// 通过代理再取子播放列表，里面的分片地址应继续指向代理。
	variantResp, err := http.Get(base + variantPath)
	if err != nil {
		t.Fatalf("请求子播放列表失败: %v", err)
	}
	defer variantResp.Body.Close()
	variantBody, err := io.ReadAll(variantResp.Body)
	if err != nil {
		t.Fatalf("读取子播放列表失败: %v", err)
	}
	wantSegment := proxyRefPath(upstream.URL + "/live/chunk/seg-1.ts")
	if !strings.Contains(string(variantBody), wantSegment) {
		t.Errorf("分片地址未重写，子播放列表:\n%s", variantBody)
	}

	segmentResp, err := http.Get(base + strings.TrimSpace(strings.Split(string(variantBody), "\n")[2]))
	if err != nil {
		t.Fatalf("请求代理分片失败: %v", err)
	}
	defer segmentResp.Body.Close()
	segmentBody, err := io.ReadAll(segmentResp.Body)
	if err != nil {
		t.Fatalf("读取代理分片失败: %v", err)
	}
	if string(segmentBody) != "SEGMENT" {
		t.Fatalf("分片内容 = %q, want SEGMENT", segmentBody)
	}
	if got := segmentResp.Header.Get("Content-Type"); got != "video/mp2t" {
		t.Fatalf("分片 Content-Type = %q, want video/mp2t", got)
	}
}

func TestTVProxyRejectsInvalidRequests(t *testing.T) {
	base, closeProxy := newTestProxy(t)
	defer closeProxy()

	cases := []struct {
		name     string
		method   string
		query    string
		wantCode int
	}{
		{name: "缺少地址", method: http.MethodGet, query: "", wantCode: http.StatusBadRequest},
		{name: "非 http 协议", method: http.MethodGet, query: "ftp://example.com/a.m3u8", wantCode: http.StatusBadRequest},
		{name: "方法不支持", method: http.MethodPost, query: "http://example.com/a.m3u8", wantCode: http.StatusMethodNotAllowed},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest(c.method, base+"/tv/proxy?src="+url.QueryEscape(c.query), nil)
			if err != nil {
				t.Fatalf("构造请求失败: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != c.wantCode {
				t.Fatalf("状态码 = %d, want %d", resp.StatusCode, c.wantCode)
			}
		})
	}
}
