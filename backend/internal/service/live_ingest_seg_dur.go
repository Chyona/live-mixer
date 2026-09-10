package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"go.uber.org/zap"
)

const segDurationsFileName = "seg_durations.json"

func loadSegDurations(dir string) map[int64]int64 {
	out := map[int64]int64{}
	b, err := os.ReadFile(filepath.Join(dir, segDurationsFileName))
	if err != nil || len(b) == 0 {
		return out
	}
	raw := map[string]int64{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return out
	}
	for k, v := range raw {
		idx, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			continue
		}
		out[idx] = v
	}
	return out
}

func saveSegDurations(dir string, dur map[int64]int64) {
	if len(dur) == 0 {
		return
	}
	raw := make(map[string]int64, len(dur))
	for k, v := range dur {
		raw[strconv.FormatInt(k, 10)] = v
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, segDurationsFileName), b, 0o644)
}

func mergeSegDurations(dst map[int64]int64, src map[int64]int64) {
	for k, v := range src {
		if v > 0 {
			dst[k] = v
		}
	}
}

func (w *liveIngestWorker) persistSegDurations(workDir string, segDurMS map[int64]int64) {
	saveSegDurations(workDir, segDurMS)
}

func (w *liveIngestWorker) loadPersistedSegDurations(workDir string, into map[int64]int64) {
	loaded := loadSegDurations(workDir)
	if len(loaded) == 0 {
		return
	}
	mergeSegDurations(into, loaded)
	w.logger.Debug("已加载分片时长 sidecar",
		zap.String("work_dir", workDir),
		zap.Int("count", len(loaded)),
	)
}
