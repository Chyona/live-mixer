package service

import (
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"

	"go.uber.org/zap"
)

func loadSegDurations(dir string) map[int64]int64 {
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	return liveingest.LoadMediaTimelineIndex(dir, nominal).DurationMap()
}

func saveSegDurations(dir string, dur map[int64]int64) {
	if len(dur) == 0 {
		return
	}
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	idx := liveingest.LoadMediaTimelineIndex(dir, nominal)
	idx.MergeDurations(dur)
	_ = liveingest.SaveMediaTimelineIndex(dir, idx)
}

func mergeSegDurations(dst map[int64]int64, src map[int64]int64) {
	liveingest.MergeDurationsInto(dst, src)
}

// persistSegDurations 将内存时长 map 写入 MediaTimelineIndex（并兼容旧 seg_durations.json）。
func (w *liveIngestWorker) persistSegDurations(workDir string, segDurMS map[int64]int64) {
	if workDir == "" || len(segDurMS) == 0 {
		return
	}
	saveSegDurations(workDir, segDurMS)
}

func (w *liveIngestWorker) loadPersistedSegDurations(workDir string, into map[int64]int64) {
	if into == nil {
		return
	}
	loaded := loadSegDurations(workDir)
	mergeSegDurations(into, loaded)
	if w != nil && w.logger != nil && len(loaded) > 0 {
		w.logger.Debug("已加载媒体时间轴索引",
			zap.String("work_dir", workDir),
			zap.Int("count", len(loaded)),
		)
	}
}

// loadTimelineIndex 读取分片目录的 MediaTimelineIndex。
func loadTimelineIndex(workDir string) *liveingest.MediaTimelineIndex {
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	return liveingest.LoadMediaTimelineIndex(workDir, nominal)
}
