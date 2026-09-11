package liveingest

import (
	"fmt"
	"strings"
	"time"

	"live-mixer/internal/model"
)

// BuildWindowsPlaylist 由已就绪媒体窗生成 EVENT HLS（条目为同源 TS URL）。
func BuildWindowsPlaylist(windows model.MediaWindowList, targetDurationSec int, ended bool) string {
	if targetDurationSec <= 0 {
		targetDurationSec = int(model.LiveMediaWindowDuration / time.Second)
		if targetDurationSec <= 0 {
			targetDurationSec = 600
		}
	}
	var maxDur float64
	for _, w := range windows {
		if !w.Ready {
			continue
		}
		d := float64(w.DurMS) / 1000.0
		if d > maxDur {
			maxDur = d
		}
	}
	td := targetDurationSec
	if int(maxDur+0.999) > td {
		td = int(maxDur + 0.999)
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:EVENT\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", td))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	for _, w := range windows {
		if !w.Ready {
			continue
		}
		u := strings.TrimSpace(w.TSURL)
		if u == "" {
			u = strings.TrimSpace(w.URL)
		}
		if u == "" {
			continue
		}
		dur := float64(w.DurMS) / 1000.0
		if dur <= 0 {
			dur = float64(td)
		}
		b.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", dur))
		b.WriteString(u)
		b.WriteString("\n")
	}
	if ended {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	return b.String()
}
