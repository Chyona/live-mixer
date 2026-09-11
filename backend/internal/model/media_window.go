package model

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MediaWindow 跟播媒体窗元数据（落库 jsonb）。
// 每个窗口对应一份 MP4：预览 / ASR / 一键成片同源。
type MediaWindow struct {
	Index     int    `json:"i"`
	StartMS   int64  `json:"start_ms"`
	EndMS     int64  `json:"end_ms"`
	DurMS     int64  `json:"dur_ms"`
	URL       string `json:"url,omitempty"`
	TSURL     string `json:"ts_url,omitempty"` // HLS 预览用（与 MP4 同源）
	ObjectKey string `json:"object_key,omitempty"`
	TSKey     string `json:"ts_key,omitempty"`
	SegStart  int64  `json:"seg_start"`
	SegEnd    int64  `json:"seg_end"` // exclusive
	Ready     bool   `json:"ready"`
}

// MediaWindowList 有序窗列表。
type MediaWindowList []MediaWindow

// ParseMediaWindows 解析 jsonb。
func ParseMediaWindows(raw string) MediaWindowList {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	var out MediaWindowList
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// Marshal 序列化。
func (l MediaWindowList) Marshal() string {
	if len(l) == 0 {
		return "[]"
	}
	b, err := json.Marshal(l)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ReadyCount 已就绪窗数量。
func (l MediaWindowList) ReadyCount() int {
	n := 0
	for _, w := range l {
		if w.Ready {
			n++
		}
	}
	return n
}

// TotalReadyMS 已就绪窗覆盖的全局终点毫秒。
func (l MediaWindowList) TotalReadyMS() int64 {
	var maxEnd int64
	for _, w := range l {
		if w.Ready && w.EndMS > maxEnd {
			maxEnd = w.EndMS
		}
	}
	return maxEnd
}

// FindByGlobalMS 定位全局毫秒所在窗。
func (l MediaWindowList) FindByGlobalMS(ms int64) (MediaWindow, int64, bool) {
	if ms < 0 {
		ms = 0
	}
	for _, w := range l {
		if !w.Ready || w.DurMS <= 0 {
			continue
		}
		if ms < w.EndMS {
			off := ms - w.StartMS
			if off < 0 {
				off = 0
			}
			return w, off, true
		}
	}
	if len(l) > 0 {
		last := l[len(l)-1]
		if last.Ready {
			off := last.DurMS
			if ms <= last.EndMS {
				off = ms - last.StartMS
				if off < 0 {
					off = 0
				}
			}
			return last, off, true
		}
	}
	return MediaWindow{}, 0, false
}

// MediaWindowSpan 落在单窗内的一段。
type MediaWindowSpan struct {
	Window   MediaWindow
	OffsetMS int64
	DurMS    int64
}

// ResolveRange 将 [start,end) 拆到各窗内的局部区间。
func (l MediaWindowList) ResolveRange(startMS, endMS int64) ([]MediaWindowSpan, error) {
	if endMS <= startMS {
		return nil, fmt.Errorf("无效时间范围: %d-%d", startMS, endMS)
	}
	var spans []MediaWindowSpan
	cur := startMS
	guard := 0
	for cur < endMS {
		guard++
		if guard > 10000 {
			return nil, fmt.Errorf("ResolveRange 异常循环")
		}
		w, off, ok := l.FindByGlobalMS(cur)
		if !ok || !w.Ready {
			return nil, fmt.Errorf("全局时间 %d 无就绪媒体窗", cur)
		}
		left := w.DurMS - off
		if left <= 0 {
			cur = w.EndMS + 1
			continue
		}
		take := left
		if cur+take > endMS {
			take = endMS - cur
		}
		spans = append(spans, MediaWindowSpan{Window: w, OffsetMS: off, DurMS: take})
		cur += take
	}
	if len(spans) == 0 {
		return nil, fmt.Errorf("无法解析时间范围 %d-%d", startMS, endMS)
	}
	return spans, nil
}

// NextPendingASRWindow 返回第一个 EndMS > cursor 的就绪窗。
func (l MediaWindowList) NextPendingASRWindow(cursorMS int64) (MediaWindow, bool) {
	for _, w := range l {
		if w.Ready && w.EndMS > cursorMS {
			return w, true
		}
	}
	return MediaWindow{}, false
}

// Upsert 按 Index 更新或追加。
func (l MediaWindowList) Upsert(w MediaWindow) MediaWindowList {
	for i := range l {
		if l[i].Index == w.Index {
			l[i] = w
			sort.Slice(l, func(a, b int) bool { return l[a].Index < l[b].Index })
			return l
		}
	}
	l = append(l, w)
	sort.Slice(l, func(a, b int) bool { return l[a].Index < l[b].Index })
	return l
}
