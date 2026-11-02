package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// defaultTarget 默认直连重试访问的 IPTV 订阅源，用于确认不开代理时能否拉取成功。
const defaultTarget = "https://raw.githubusercontent.com/cs3306/IPTV-Sources/main/data/output/iptv_collection.m3u"

func main() {
	target := flag.String("url", defaultTarget, "要访问的地址")
	maxAttempts := flag.Int("max", 0, "最大尝试次数，0 表示一直重试直到成功")
	interval := flag.Duration("interval", 3*time.Second, "重试间隔")
	timeout := flag.Duration("timeout", 20*time.Second, "单次请求超时")
	flag.Parse()

	fmt.Printf("检测地址：%s\n", *target)

	// 显式不使用系统代理，模拟“不开梯子”的直连环境。
	client := &http.Client{
		Timeout: *timeout,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         (&net.Dialer{Timeout: 8 * time.Second}).DialContext,
			TLSHandshakeTimeout: 8 * time.Second,
		},
	}

	for attempt := 1; ; attempt++ {
		start := time.Now()
		req, err := http.NewRequest(http.MethodGet, *target, nil)
		if err != nil {
			fmt.Printf("构造请求失败: %v\n", err)
			os.Exit(2)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) bing-play/iptv-check")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("第 %d 次失败: %v（耗时 %s）\n", attempt, err, time.Since(start).Round(time.Millisecond))
		} else {
			n, copyErr := io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && copyErr == nil && n > 0 {
				fmt.Printf("第 %d 次成功: HTTP %d，%d 字节，总耗时 %s\n",
					attempt, resp.StatusCode, n, time.Since(start).Round(time.Millisecond))
				return
			}
			fmt.Printf("第 %d 次失败: HTTP %d，%d 字节，读取错误 %v（耗时 %s）\n",
				attempt, resp.StatusCode, n, copyErr, time.Since(start).Round(time.Millisecond))
		}

		if *maxAttempts > 0 && attempt >= *maxAttempts {
			fmt.Printf("已尝试 %d 次仍未访问成功，退出\n", attempt)
			os.Exit(1)
		}
		time.Sleep(*interval)
	}
}
