package liveingest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const (
	// TimelineIndexFileName 媒体时间轴索引 sidecar（与分片同目录）。
	TimelineIndexFileName = "timeline_index.json"
	// SegDurationsFileName 旧版分片时长 map；Load 时兼容迁移。
	SegDurationsFileName = "seg_durations.json"
)

// TimelineSeg 单片在 Index 轴上的时长与累计终点。
type TimelineSeg struct {
	Index    int64 `json:"i"`
	DurMS    int64 `json:"dur_ms"`
	CumEndMS int64 `json:"cum_end_ms"`
}

// MediaTimelineIndex 跟播分片的唯一媒体时间轴（毫秒）。
type MediaTimelineIndex struct {
	Version      int           `json:"version"`
	NominalSegMS int64         `json:"nominal_seg_ms"`
	Segs         []TimelineSeg `json:"segs"`
	TotalMS      int64         `json:"total_ms"`
}

// SegSpan 表示 Index 轴上一段落在单片内的区间。
type SegSpan struct {
	SegIndex int64
	OffsetMS int64 // 片内起点
	DurMS    int64 // 本 span 时长
	FilePath string
}

// NewMediaTimelineIndex 创建空索引。
func NewMediaTimelineIndex(nominalSegMS int64) *MediaTimelineIndex {
	if nominalSegMS <= 0 {
		nominalSegMS = 6000
	}
	return &MediaTimelineIndex{Version: 1, NominalSegMS: nominalSegMS, Segs: nil, TotalMS: 0}
}

// SetDuration 写入/更新某片时长并重算累计。
func (idx *MediaTimelineIndex) SetDuration(segIndex, durMS int64) {
	if idx == nil || segIndex < 0 || durMS <= 0 {
		return
	}
	if idx.NominalSegMS <= 0 {
		idx.NominalSegMS = 6000
	}
	if idx.Version == 0 {
		idx.Version = 1
	}
	found := false
	for i := range idx.Segs {
		if idx.Segs[i].Index == segIndex {
			idx.Segs[i].DurMS = durMS
			found = true
			break
		}
	}
	if !found {
		idx.Segs = append(idx.Segs, TimelineSeg{Index: segIndex, DurMS: durMS})
	}
	idx.recompute()
}

// MergeDurations 批量合并时长 map。
func (idx *MediaTimelineIndex) MergeDurations(durs map[int64]int64) {
	for k, v := range durs {
		if v > 0 {
			idx.SetDuration(k, v)
		}
	}
}

func (idx *MediaTimelineIndex) recompute() {
	sort.Slice(idx.Segs, func(i, j int) bool { return idx.Segs[i].Index < idx.Segs[j].Index })
	var cum int64
	out := make([]TimelineSeg, 0, len(idx.Segs))
	expect := int64(0)
	for _, s := range idx.Segs {
		// 稠密填充缺失下标，用标称时长占位，保证 Resolve 连续。
		for expect < s.Index {
			cum += idx.NominalSegMS
			out = append(out, TimelineSeg{Index: expect, DurMS: idx.NominalSegMS, CumEndMS: cum})
			expect++
		}
		if s.DurMS <= 0 {
			s.DurMS = idx.NominalSegMS
		}
		cum += s.DurMS
		s.CumEndMS = cum
		out = append(out, s)
		expect = s.Index + 1
	}
	idx.Segs = out
	idx.TotalMS = cum
}

// DurationMap 返回 segIndex → dur_ms（兼容旧 sidecar / playlist）。
func (idx *MediaTimelineIndex) DurationMap() map[int64]int64 {
	out := map[int64]int64{}
	if idx == nil {
		return out
	}
	for _, s := range idx.Segs {
		if s.DurMS > 0 {
			out[s.Index] = s.DurMS
		}
	}
	return out
}

// CumStartMS 返回分片起点累计毫秒。
func (idx *MediaTimelineIndex) CumStartMS(segIndex int64) int64 {
	if idx == nil || segIndex <= 0 {
		return 0
	}
	for _, s := range idx.Segs {
		if s.Index == segIndex-1 {
			return s.CumEndMS
		}
		if s.Index >= segIndex {
			break
		}
	}
	// 未命中时用标称网格估算。
	if idx.NominalSegMS > 0 {
		return segIndex * idx.NominalSegMS
	}
	return 0
}

// WindowMS 返回 [startSeg, endSegExclusive) 的 Index 轴时长。
func (idx *MediaTimelineIndex) WindowMS(startSeg, endSegExclusive int64) int64 {
	if idx == nil || endSegExclusive <= startSeg {
		return 0
	}
	start := idx.CumStartMS(startSeg)
	end := idx.CumStartMS(endSegExclusive)
	if end <= start {
		// endSeg 可能超出已知 segs：用 total 或补标称。
		if endSegExclusive > 0 && len(idx.Segs) > 0 {
			last := idx.Segs[len(idx.Segs)-1]
			if endSegExclusive > last.Index+1 {
				end = last.CumEndMS + (endSegExclusive-last.Index-1)*idx.NominalSegMS
			} else {
				end = last.CumEndMS
			}
		}
	}
	if end < start {
		return 0
	}
	return end - start
}

// Resolve 将 Index 轴毫秒映射为 (segIndex, 片内 offset)。
func (idx *MediaTimelineIndex) Resolve(ms int64) (segIndex, offsetMS int64, ok bool) {
	if idx == nil {
		return 0, 0, false
	}
	if ms < 0 {
		ms = 0
	}
	if len(idx.Segs) == 0 {
		if idx.NominalSegMS <= 0 {
			return 0, 0, false
		}
		segIndex = ms / idx.NominalSegMS
		offsetMS = ms % idx.NominalSegMS
		return segIndex, offsetMS, true
	}
	if ms >= idx.TotalMS {
		last := idx.Segs[len(idx.Segs)-1]
		// 落在最后一片末尾（允许等于 total 时夹到片尾前 0ms）。
		off := last.DurMS - 1
		if off < 0 {
			off = 0
		}
		if ms == idx.TotalMS {
			return last.Index, last.DurMS, true
		}
		return last.Index, off, true
	}
	var prevCum int64
	for _, s := range idx.Segs {
		if ms < s.CumEndMS {
			return s.Index, ms - prevCum, true
		}
		prevCum = s.CumEndMS
	}
	last := idx.Segs[len(idx.Segs)-1]
	return last.Index, last.DurMS, true
}

// Range 将 [startMS, endMS) 拆成若干 SegSpan（不含 FilePath）。
func (idx *MediaTimelineIndex) Range(startMS, endMS int64) ([]SegSpan, error) {
	if idx == nil {
		return nil, fmt.Errorf("timeline index 为空")
	}
	if endMS <= startMS {
		return nil, fmt.Errorf("无效时间范围: start=%d end=%d", startMS, endMS)
	}
	if startMS < 0 {
		startMS = 0
	}
	var spans []SegSpan
	remain := endMS - startMS
	cur := startMS
	for remain > 0 {
		seg, off, ok := idx.Resolve(cur)
		if !ok {
			return nil, fmt.Errorf("无法解析时间点 %d", cur)
		}
		dur := idx.segDur(seg)
		if dur <= 0 {
			dur = idx.NominalSegMS
		}
		leftInSeg := dur - off
		if leftInSeg <= 0 {
			// 恰在片边界：进下一片。
			cur = idx.CumStartMS(seg + 1)
			continue
		}
		take := leftInSeg
		if take > remain {
			take = remain
		}
		spans = append(spans, SegSpan{SegIndex: seg, OffsetMS: off, DurMS: take})
		remain -= take
		cur += take
	}
	return spans, nil
}

func (idx *MediaTimelineIndex) segDur(segIndex int64) int64 {
	for _, s := range idx.Segs {
		if s.Index == segIndex {
			return s.DurMS
		}
	}
	return idx.NominalSegMS
}

// AttachFilePaths 为 spans 填本地路径。
func AttachFilePaths(dir string, spans []SegSpan) []SegSpan {
	out := make([]SegSpan, len(spans))
	copy(out, spans)
	for i := range out {
		out[i].FilePath = filepath.Join(dir, SegmentFileName(int(out[i].SegIndex)))
	}
	return out
}

// SaveMediaTimelineIndex 原子写入 timeline_index.json，并同步旧版 seg_durations.json。
func SaveMediaTimelineIndex(dir string, idx *MediaTimelineIndex) error {
	if idx == nil || dir == "" {
		return fmt.Errorf("index 或目录为空")
	}
	idx.recompute()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, TimelineIndexFileName+".partial")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	final := filepath.Join(dir, TimelineIndexFileName)
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(final)
		if err2 := os.Rename(tmp, final); err2 != nil {
			return err2
		}
	}
	// 兼容旧读者：同步 seg_durations.json
	_ = saveSegDurationsCompat(dir, idx.DurationMap())
	return nil
}

func saveSegDurationsCompat(dir string, dur map[int64]int64) error {
	if len(dur) == 0 {
		return nil
	}
	raw := make(map[string]int64, len(dur))
	for k, v := range dur {
		raw[strconv.FormatInt(k, 10)] = v
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, SegDurationsFileName), b, 0o644)
}

// LoadMediaTimelineIndex 读取 timeline_index.json；不存在则从 seg_durations.json 迁移。
func LoadMediaTimelineIndex(dir string, nominalSegMS int64) *MediaTimelineIndex {
	idx := NewMediaTimelineIndex(nominalSegMS)
	if dir == "" {
		return idx
	}
	b, err := os.ReadFile(filepath.Join(dir, TimelineIndexFileName))
	if err == nil && len(b) > 0 {
		var loaded MediaTimelineIndex
		if json.Unmarshal(b, &loaded) == nil && (len(loaded.Segs) > 0 || loaded.TotalMS > 0) {
			if loaded.NominalSegMS <= 0 {
				loaded.NominalSegMS = idx.NominalSegMS
			}
			if loaded.Version == 0 {
				loaded.Version = 1
			}
			loaded.recompute()
			return &loaded
		}
	}
	// 兼容旧 sidecar
	legacy := loadSegDurationsCompat(dir)
	if len(legacy) > 0 {
		idx.MergeDurations(legacy)
	}
	return idx
}

func loadSegDurationsCompat(dir string) map[int64]int64 {
	out := map[int64]int64{}
	b, err := os.ReadFile(filepath.Join(dir, SegDurationsFileName))
	if err != nil || len(b) == 0 {
		return out
	}
	raw := map[string]int64{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return out
	}
	for k, v := range raw {
		idx, err := strconv.ParseInt(k, 10, 64)
		if err != nil || v <= 0 {
			continue
		}
		out[idx] = v
	}
	return out
}

// MergeDurationsInto 将 src 合并进 dst（仅正值）。
func MergeDurationsInto(dst, src map[int64]int64) {
	if dst == nil {
		return
	}
	for k, v := range src {
		if v > 0 {
			dst[k] = v
		}
	}
}

// UpsertSegDuration 加载-更新-保存单片时长（Recorder onSeg 用）。
func UpsertSegDuration(dir string, segIndex, durMS, nominalSegMS int64) *MediaTimelineIndex {
	idx := LoadMediaTimelineIndex(dir, nominalSegMS)
	idx.SetDuration(segIndex, durMS)
	_ = SaveMediaTimelineIndex(dir, idx)
	return idx
}
