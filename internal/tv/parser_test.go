package tv

import (
	"strings"
	"testing"
)

func TestParseM3U(t *testing.T) {
	input := "\ufeff#EXTM3U\r\n" +
		"#EXTINF:-1 tvg-id=\"572136\" tvg-name=\"CCTV-11\" tvg-logo=\"https://logo/11.png\" group-title=\"中国大陆 · 央视\",CCTV-11 戏曲\r\n" +
		"#EXTVLCOPT:http-user-agent=Mozilla/5.0\r\n" +
		"http://example.com/cctv11.m3u8\r\n" +
		"#EXTGRP:地方台\r\n" +
		"#EXTINF:-1 tvg-id=\"1\",北京卫视\r\n" +
		"http://example.com/bj.m3u8\r\n" +
		"#EXTINF:-1 group-title=\"综合,高清\",测试台\r\n" +
		"http://example.com/test.m3u8\r\n" +
		"#EXTINF:-1,无地址台\r\n" +
		"# comment\r\n" +
		"#EXTINF:-1 tvg-id=\"572136\" tvg-name=\"CCTV-11\" tvg-logo=\"https://logo/11.png\" group-title=\"中国大陆 · 央视\",CCTV-11 戏曲\r\n" +
		"http://example.com/cctv11.m3u8\r\n"

	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseM3U returned error: %v", err)
	}
	if len(channels) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(channels))
	}

	first := channels[0]
	if first.TvgID != "572136" || first.TvgName != "CCTV-11" {
		t.Fatalf("unexpected tvg fields: %+v", first)
	}
	if first.Name != "CCTV-11 戏曲" {
		t.Fatalf("unexpected display name: %q", first.Name)
	}
	if first.Logo != "https://logo/11.png" {
		t.Fatalf("unexpected logo: %q", first.Logo)
	}
	if first.GroupTitle != "中国大陆 · 央视" {
		t.Fatalf("unexpected group: %q", first.GroupTitle)
	}
	if first.StreamURL != "http://example.com/cctv11.m3u8" {
		t.Fatalf("unexpected stream url: %q", first.StreamURL)
	}
	if first.Duration != -1 {
		t.Fatalf("unexpected duration: %d", first.Duration)
	}
	if first.SortOrder != 0 || first.Key == "" {
		t.Fatalf("unexpected order/key: %+v", first)
	}

	second := channels[1]
	if second.Name != "北京卫视" || second.GroupTitle != "地方台" {
		t.Fatalf("unexpected second channel: %+v", second)
	}

	third := channels[2]
	if third.GroupTitle != "综合,高清" {
		t.Fatalf("group-title 中包含逗号时解析错误: %q", third.GroupTitle)
	}
	if third.SortOrder != 2 {
		t.Fatalf("unexpected sort order: %d", third.SortOrder)
	}
}

func TestParseM3U_NameFallback(t *testing.T) {
	input := "#EXTINF:3600 tvg-name=\"标准名称\",\nhttp://example.com/stream.m3u8\n"

	channels, err := ParseM3U(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseM3U returned error: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].Name != "标准名称" {
		t.Fatalf("expected fallback to tvg-name, got %q", channels[0].Name)
	}
	if channels[0].Duration != 3600 {
		t.Fatalf("unexpected duration: %d", channels[0].Duration)
	}
}

func TestParseM3U_Empty(t *testing.T) {
	channels, err := ParseM3U(strings.NewReader("#EXTM3U\n"))
	if err != nil {
		t.Fatalf("ParseM3U returned error: %v", err)
	}
	if len(channels) != 0 {
		t.Fatalf("expected no channels, got %d", len(channels))
	}
}
