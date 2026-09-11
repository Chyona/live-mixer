package service

import (
	"context"
	"fmt"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/media"
)

// resolveWindowStartFast 按 MediaTimelineIndex 定位窗口起点（Index 轴）。
// cursor=0 时直接返回 0；否则 Resolve(cursor) 得 seg，再按 overlap 回退片起点。
// 无索引或索引过短时回退逐片探测（并写回 Index）。
func resolveWindowStartFast(
	ctx context.Context,
	prober media.MediaTimelineProber,
	dir string,
	cursorMS int64,
	overlapSegs int,
) (startSeg, offsetMS int64, err error) {
	if cursorMS <= 0 {
		return 0, 0, nil
	}
	files := globLocalSegments(dir)
	if len(files) == 0 {
		return 0, 0, fmt.Errorf("无本地分片")
	}
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	if nominal <= 0 {
		nominal = 6000
	}

	idx := loadTimelineIndex(dir)
	// 仅当 sidecar 已有真实分片时长时走 Index；空索引不填标称网格（避免与探针路径分叉）。
	if len(idx.Segs) > 0 && idx.TotalMS > 0 {
		resolveAt := cursorMS
		if resolveAt >= idx.TotalMS {
			resolveAt = idx.TotalMS - 1
			if resolveAt < 0 {
				resolveAt = 0
			}
		}
		seg, _, ok := idx.Resolve(resolveAt)
		if ok {
			start := seg
			if overlapSegs > 0 {
				start -= int64(overlapSegs)
				if start < 0 {
					start = 0
				}
			}
			return start, idx.CumStartMS(start), nil
		}
	}

	// 回退：逐片探测并写回 Index。
	if prober == nil {
		prober = media.NewFFprobeProber("")
	}
	byIdx := make(map[int]string, len(files))
	maxIdx := -1
	for _, f := range files {
		idxN, ok := parseLocalSegIndex(f)
		if !ok || idxN < 0 {
			continue
		}
		byIdx[idxN] = f
		if idxN > maxIdx {
			maxIdx = idxN
		}
	}
	if maxIdx < 0 {
		return 0, 0, fmt.Errorf("无有效分片名")
	}

	durs := make([]int64, maxIdx+1)
	var cum int64
	hitSeg := int64(maxIdx + 1)
	for i := 0; i <= maxIdx; i++ {
		dur := nominal
		if p, ok := byIdx[i]; ok {
			dur = probeSegmentDurationMS(ctx, prober, p, nominal)
			liveingest.UpsertSegDuration(dir, int64(i), dur, nominal)
		}
		durs[i] = dur
		if cum+dur > cursorMS {
			hitSeg = int64(i)
			break
		}
		cum += dur
		hitSeg = int64(i + 1)
	}

	start := hitSeg
	if overlapSegs > 0 {
		start -= int64(overlapSegs)
		if start < 0 {
			start = 0
		}
	}
	var off int64
	for i := int64(0); i < start && int(i) < len(durs); i++ {
		d := durs[i]
		if d <= 0 {
			d = nominal
		}
		off += d
	}
	return start, off, nil
}

// ensureIndexCoversFiles 用标称时长补齐已落盘分片的最大下标，便于 Resolve。
func ensureIndexCoversFiles(idx *liveingest.MediaTimelineIndex, files []string) {
	if idx == nil {
		return
	}
	maxIdx := int64(-1)
	for _, f := range files {
		n, ok := parseLocalSegIndex(f)
		if ok && int64(n) > maxIdx {
			maxIdx = int64(n)
		}
	}
	if maxIdx < 0 {
		return
	}
	if len(idx.Segs) == 0 || idx.Segs[len(idx.Segs)-1].Index < maxIdx {
		d := idx.NominalSegMS
		if d <= 0 {
			d = 6000
		}
		// 若该片已有真实时长则保留。
		for _, s := range idx.Segs {
			if s.Index == maxIdx && s.DurMS > 0 {
				d = s.DurMS
				break
			}
		}
		idx.SetDuration(maxIdx, d)
	}
}

func probeSegmentDurationMS(ctx context.Context, prober media.MediaTimelineProber, path string, fallback int64) int64 {
	if path == "" || prober == nil {
		return fallback
	}
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tl, err := prober.ProbeMediaTimeline(pctx, path)
	if err != nil {
		return fallback
	}
	if d := tl.DurationMS(); d > 0 {
		return d
	}
	return fallback
}
