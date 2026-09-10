package service

import (
	"context"
	"fmt"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/media"
)

// resolveWindowStartFast 按真实分片时长定位窗口起点。
// cursor=0 时直接返回 0；否则按分片下标递增探测，覆盖 cursor 后立即停止。
// 禁止对「全部历史分片」做全量 ffprobe——长直播时会卡死 asrBusy，导致进度一直 0%。
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
	if prober == nil {
		prober = media.NewFFprobeProber("")
	}
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	if nominal <= 0 {
		nominal = 6000
	}

	byIdx := make(map[int]string, len(files))
	maxIdx := -1
	for _, f := range files {
		idx, ok := parseLocalSegIndex(f)
		if !ok || idx < 0 {
			continue
		}
		byIdx[idx] = f
		if idx > maxIdx {
			maxIdx = idx
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
