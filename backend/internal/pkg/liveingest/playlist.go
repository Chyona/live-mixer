package liveingest

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// PlaylistItem 播放列表中的一片。
type PlaylistItem struct {
	DurationSec float64
	URL         string
}

var playlistSegEpochRe = regexp.MustCompile(`/seg/(\d+)/`)

// PlaylistSegmentEpoch 从分片 URL 取出跟播目录代数（.../seg/{epoch}/seg_00000.ts）。
func PlaylistSegmentEpoch(rawURL string) string {
	m := playlistSegEpochRe.FindStringSubmatch(rawURL)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// BuildEventPlaylist 生成可边录边播的 EVENT 列表；ended 时写入 ENDLIST。
// 仅在分片目录代数变化（续录换 epoch）时写入 DISCONTINUITY；同一路连续分片不要逐片插入。
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
	prevEpoch := ""
	for _, it := range items {
		epoch := PlaylistSegmentEpoch(it.URL)
		if prevEpoch != "" && epoch != "" && epoch != prevEpoch {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		if epoch != "" {
			prevEpoch = epoch
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

// segmentDurationMS 跟播分片时长（毫秒）。
func segmentDurationMS(segmentSec int) int64 {
	if segmentSec <= 0 {
		segmentSec = 6
	}
	return int64(segmentSec) * int64(time.Second/time.Millisecond)
}

// WindowStartIndex 计算窗口 ASR 对应的起始分片（含）。
// 与拼接音频起点一致：cursor 落在分片中间时向下取整到该分片开头。
//
// 注意：仅当「游标时间轴」与「标称分片时长」一致时才准确。
// 跟播真实分片常偏离 6s，应用 ResolveWindowStartByDurations 按实测时长定位。
func WindowStartIndex(cursorMS int64, segmentSec int) int64 {
	segMS := segmentDurationMS(segmentSec)
	if segMS <= 0 || cursorMS <= 0 {
		return 0
	}
	return cursorMS / segMS
}

// WindowOffsetMS 窗口 ASR 结果应平移的毫秒数（标称分片网格）。
// 必须等于实际拼接音频的时间原点（startSeg×分片时长），不能直接用 asr_cursor_ms：
// cursor 常落在分片中间，若用裸 cursor 平移会造成字幕相对音频整体偏移（最大接近一个分片）。
func WindowOffsetMS(cursorMS int64, segmentSec int) int64 {
	return WindowStartIndex(cursorMS, segmentSec) * segmentDurationMS(segmentSec)
}

// ResolveWindowStartByDurations 按各分片真实时长，将 asr_cursor_ms 映射为下一窗起始下标与时间原点。
// segmentDurationsMS[i] 为分片 i 的时长（毫秒），应按索引稠密填充；未知可用标称时长。
// overlapSegs>0 时向前多叠若干片，便于窗边界连续（重复句由 MergeWindowASR 的 skipBefore 去掉）。
//
// 旧逻辑 startSeg=cursor/(标称6s) 在真实分片≠6s 时会在窗边界跳段或重叠，表现为「前 N 分钟准、之后整段错位」。
func ResolveWindowStartByDurations(segmentDurationsMS []int64, cursorMS int64, overlapSegs int) (startSeg, offsetMS int64) {
	if cursorMS <= 0 || len(segmentDurationsMS) == 0 {
		return 0, 0
	}
	var cum int64
	seg := int64(len(segmentDurationsMS))
	for i, d := range segmentDurationsMS {
		if d < 0 {
			d = 0
		}
		if cum+d > cursorMS {
			seg = int64(i)
			break
		}
		cum += d
		seg = int64(i + 1)
	}
	if overlapSegs > 0 {
		seg -= int64(overlapSegs)
		if seg < 0 {
			seg = 0
		}
	}
	if int(seg) > len(segmentDurationsMS) {
		seg = int64(len(segmentDurationsMS))
	}
	var off int64
	for i := int64(0); i < seg; i++ {
		if segmentDurationsMS[i] > 0 {
			off += segmentDurationsMS[i]
		}
	}
	return seg, off
}

// SumSegmentDurationsMS 累加 [start, start+count) 的分片时长。
func SumSegmentDurationsMS(segmentDurationsMS []int64, startSeg int64, count int) int64 {
	if count <= 0 || startSeg < 0 || int(startSeg) >= len(segmentDurationsMS) {
		return 0
	}
	end := int(startSeg) + count
	if end > len(segmentDurationsMS) {
		end = len(segmentDurationsMS)
	}
	var sum int64
	for i := int(startSeg); i < end; i++ {
		if segmentDurationsMS[i] > 0 {
			sum += segmentDurationsMS[i]
		}
	}
	return sum
}
