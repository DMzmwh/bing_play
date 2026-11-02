package tv

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Channel M3U 中的一条直播频道。
type Channel struct {
	// TvgID EPG 频道标识，对应 tvg-id。
	TvgID string
	// TvgName 频道标准名称，对应 tvg-name。
	TvgName string
	// Name 用户看到的显示名，取自 #EXTINF 逗号后的内容。
	Name string
	// Logo 台标地址，对应 tvg-logo。
	Logo string
	// GroupTitle 频道分组，对应 group-title；缺失时回退 #EXTGRP。
	GroupTitle string
	// StreamURL 直播流地址。
	StreamURL string
	// Duration #EXTINF 中的时长，直播通常为 -1。
	Duration int
	// SortOrder 在文件中的原始顺序。
	SortOrder int
	// Key 频道唯一标识，用于入库去重。
	Key string
}

// attributePattern 匹配 #EXTINF 中的属性：key="value" 或 key=value。
var attributePattern = regexp.MustCompile(`([A-Za-z0-9_-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s,]+))`)

// ParseM3U 解析 M3U 直播列表。
//
// 兼容 UTF-8 BOM、CRLF、#EXTGRP、#EXTVLCOPT 等常见写法；
// 没有播放地址的条目会被丢弃，完全重复的频道按 Key 去重。
func ParseM3U(r io.Reader) ([]Channel, error) {
	scanner := bufio.NewScanner(r)
	// 部分订阅源单行包含大量属性，放大缓冲区避免 ErrTooLong。
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var (
		channels []Channel
		pending  *Channel
		extGroup string
		urlSeen  bool
		lookup   = make(map[string]struct{})
	)

	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" {
			continue
		}

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "#EXTINF"):
			// 上一条没有播放地址时直接丢弃。
			pending = parseExtInf(line)
			pending.GroupTitle = extGroupIfEmpty(pending.GroupTitle, extGroup)
			urlSeen = false

		case strings.HasPrefix(upper, "#EXTGRP"):
			if idx := strings.Index(line, ":"); idx >= 0 {
				extGroup = strings.TrimSpace(line[idx+1:])
			}
			if pending != nil && pending.GroupTitle == "" {
				pending.GroupTitle = extGroup
			}

		case strings.HasPrefix(line, "#"):
			// #EXTM3U、#EXTVLCOPT、注释等对入库没有帮助，直接跳过。

		default:
			if pending == nil || urlSeen {
				// 没有 #EXTINF 的裸地址，或同一条目后跟的备用地址，均忽略。
				continue
			}
			urlSeen = true
			pending.StreamURL = line
			pending.GroupTitle = extGroupIfEmpty(pending.GroupTitle, extGroup)

			channel := *pending
			channel.Name = firstNonEmpty(channel.Name, channel.TvgName, channel.TvgID, channel.StreamURL)
			if channel.Name == "" {
				continue
			}
			channel.Key = channelKey(channel.TvgID, channel.TvgName, channel.Name, channel.StreamURL)
			if _, ok := lookup[channel.Key]; ok {
				continue
			}
			lookup[channel.Key] = struct{}{}
			channel.SortOrder = len(channels)
			channels = append(channels, channel)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return channels, nil
}

// parseExtInf 解析 #EXTINF 行。
//
// 格式： #EXTINF:<时长> 属性列表,显示名
func parseExtInf(line string) *Channel {
	channel := &Channel{Duration: -1}

	rest := line
	if idx := strings.Index(rest, ":"); idx >= 0 {
		rest = rest[idx+1:]
	}

	header, title := splitTitle(rest)
	channel.Name = strings.TrimSpace(title)

	if fields := strings.Fields(header); len(fields) > 0 {
		if duration, err := strconv.Atoi(strings.TrimSpace(fields[0])); err == nil {
			channel.Duration = duration
		}
	}

	for _, match := range attributePattern.FindAllStringSubmatch(header, -1) {
		key := strings.ToLower(match[1])
		value := attributeValue(match[0])
		switch key {
		case "tvg-id":
			channel.TvgID = value
		case "tvg-name":
			channel.TvgName = value
		case "tvg-logo":
			channel.Logo = value
		case "group-title":
			channel.GroupTitle = value
		}
	}

	return channel
}

// splitTitle 在引号之外查找第一个逗号，拆出属性段与显示名。
// group-title 的值可能含有逗号，因此不能直接按逗号切分。
func splitTitle(s string) (header string, title string) {
	var quote rune
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ',':
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// attributeValue 从 key=value 片段中取值，兼容带引号与不带引号两种写法。
func attributeValue(raw string) string {
	idx := strings.Index(raw, "=")
	if idx < 0 {
		return ""
	}
	value := strings.TrimSpace(raw[idx+1:])
	return strings.Trim(value, `"'`)
}

func extGroupIfEmpty(group, extGroup string) string {
	if group != "" {
		return group
	}
	return extGroup
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// channelKey 生成频道唯一标识：tvg-id + tvg-name + 显示名 + 播放地址。
func channelKey(tvgID, tvgName, name, streamURL string) string {
	payload := strings.Join([]string{tvgID, tvgName, name, streamURL}, "\x1f")
	sum := sha1.Sum([]byte(payload))
	return hex.EncodeToString(sum[:])
}
