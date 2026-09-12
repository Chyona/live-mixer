package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MediaWindow 跟播主 MP4 元数据（落库 media_windows jsonb，始终至多一条 ready 记录）。
// 主文件按 10/20/30… 分钟递增；预览 / ASR / 一键成片同源。
type MediaWindow struct {
	Index     int    `json:"i"`
	StartMS   int64  `json:"start_ms"`
	EndMS     int64  `json:"end_ms"`
	DurMS     int64  `json:"dur_ms"`
	URL       string `json:"url,omitempty"`
	ObjectKey string `json:"object_key,omitempty"`
	SegStart  int64  `json:"seg_start"`
	SegEnd    int64  `json:"seg_end"` // exclusive：已封入主 MP4 的分片上界
	Ready     bool   `json:"ready"`
}

// MediaWindowList 主 MP4 列表（重构后长度 0 或 1）。
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

// Master 返回就绪的主 MP4 元数据。
func (l MediaWindowList) Master() (MediaWindow, bool) {
	for _, w := range l {
		if w.Ready && w.DurMS > 0 && strings.TrimSpace(w.URL) != "" {
			return w, true
		}
	}
	for _, w := range l {
		if w.Ready && w.DurMS > 0 {
			return w, true
		}
	}
	return MediaWindow{}, false
}

// ReadyCount 已就绪主文件数量（0 或 1）。
func (l MediaWindowList) ReadyCount() int {
	if _, ok := l.Master(); ok {
		return 1
	}
	return 0
}

// TotalReadyMS 主 MP4 已覆盖的全局终点毫秒（即主文件时长）。
func (l MediaWindowList) TotalReadyMS() int64 {
	if m, ok := l.Master(); ok {
		return m.EndMS
	}
	return 0
}

// FindByGlobalMS 定位全局毫秒在主 MP4 内的偏移。
func (l MediaWindowList) FindByGlobalMS(ms int64) (MediaWindow, int64, bool) {
	m, ok := l.Master()
	if !ok {
		return MediaWindow{}, 0, false
	}
	if ms < 0 {
		ms = 0
	}
	off := ms - m.StartMS
	if off < 0 {
		off = 0
	}
	if off > m.DurMS {
		off = m.DurMS
	}
	return m, off, true
}

// MediaWindowSpan 主文件内一段。
type MediaWindowSpan struct {
	Window   MediaWindow
	OffsetMS int64
	DurMS    int64
}

// ResolveRange 将 [start,end) 映射到主 MP4 局部区间。
func (l MediaWindowList) ResolveRange(startMS, endMS int64) ([]MediaWindowSpan, error) {
	if endMS <= startMS {
		return nil, fmt.Errorf("无效时间范围: %d-%d", startMS, endMS)
	}
	m, ok := l.Master()
	if !ok {
		return nil, fmt.Errorf("主 MP4 尚未就绪")
	}
	if endMS > m.EndMS {
		return nil, fmt.Errorf("选区超出主 MP4 范围（已就绪 %dms，需要 %dms）", m.EndMS, endMS)
	}
	off := startMS - m.StartMS
	if off < 0 {
		off = 0
	}
	return []MediaWindowSpan{{
		Window:   m,
		OffsetMS: off,
		DurMS:    endMS - startMS,
	}}, nil
}

// ASRPending 主 MP4 上是否仍有未转写区间。
func (l MediaWindowList) ASRPending(cursorMS int64) bool {
	return l.TotalReadyMS() > cursorMS
}

// WithMaster 用最新主文件元数据替换列表（始终单条）。
func WithMaster(w MediaWindow) MediaWindowList {
	w.Index = 0
	w.StartMS = 0
	if w.EndMS <= 0 && w.DurMS > 0 {
		w.EndMS = w.DurMS
	}
	if w.DurMS <= 0 && w.EndMS > 0 {
		w.DurMS = w.EndMS
	}
	w.Ready = true
	return MediaWindowList{w}
}
