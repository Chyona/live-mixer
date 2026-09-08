package liveingest

import (
	"fmt"
	"strings"
	"time"
)

// PlaylistItem 播放列表中的一片。
type PlaylistItem struct {
	DurationSec float64
	URL         string
}

// BuildEventPlaylist 生成可边录边播的 EVENT 列表；ended 时写入 ENDLIST。
func BuildEventPlaylist(items []PlaylistItem, targetDuration int, ended bool) string {
	if targetDuration <= 0 {
		targetDuration = 6
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", targetDuration))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:EVENT\n")
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	for i, it := range items {
		if i > 0 {
			// 每片可能独立从 PTS 0 开始（历史 reset_timestamps / 续录），无此标记浏览器会解码失败。
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		dur := it.DurationSec
		if dur <= 0 {
			dur = float64(targetDuration)
		}
		b.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", dur))
		b.WriteString(strings.TrimSpace(it.URL))
		b.WriteString("\n")
	}
	if ended {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	return b.String()
}

// PreferStablePlaylistURL 播放列表对象会被反复覆盖，复用首次签名地址，避免前端每次轮询都换 URL 重建播放器。
func PreferStablePlaylistURL(existing, uploaded string) string {
	if u := strings.TrimSpace(existing); u != "" {
		return u
	}
	return strings.TrimSpace(uploaded)
}

// WindowStartIndex 计算窗口 ASR 对应的起始分片（含）。
func WindowStartIndex(cursorMS int64, segmentSec int) int64 {
	if segmentSec <= 0 {
		segmentSec = 6
	}
	segMS := int64(segmentSec) * int64(time.Second/time.Millisecond)
	if segMS <= 0 {
		return 0
	}
	return cursorMS / segMS
}
