package service

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type ingestCancelEntry struct {
	cancel context.CancelFunc
}

func (w *liveIngestWorker) bindRecorderCancel(materialID uint, cancel context.CancelFunc) func() {
	return w.bindRoleCancel(&w.recorderCancels, materialID, cancel)
}

func (w *liveIngestWorker) bindASRCancel(materialID uint, cancel context.CancelFunc) func() {
	return w.bindRoleCancel(&w.asrCancels, materialID, cancel)
}

func (w *liveIngestWorker) bindFinalizeCancel(materialID uint, cancel context.CancelFunc) func() {
	return w.bindRoleCancel(&w.finalizeCancels, materialID, cancel)
}

func (w *liveIngestWorker) bindRoleCancel(m *map[uint]*ingestCancelEntry, materialID uint, cancel context.CancelFunc) func() {
	entry := &ingestCancelEntry{cancel: cancel}
	w.cancelsMu.Lock()
	if *m == nil {
		*m = make(map[uint]*ingestCancelEntry)
	}
	if prev, ok := (*m)[materialID]; ok && prev != nil {
		prev.cancel()
	}
	(*m)[materialID] = entry
	w.cancelsMu.Unlock()
	return func() {
		w.cancelsMu.Lock()
		if cur, ok := (*m)[materialID]; ok && cur == entry {
			delete(*m, materialID)
		}
		w.cancelsMu.Unlock()
	}
}

// Cancel 取消本进程内该素材的录像 / 窗口 ASR / Finalize（杀 ffmpeg / 打断探测）。
func (w *liveIngestWorker) Cancel(materialID uint) bool {
	w.cancelsMu.Lock()
	var cancels []context.CancelFunc
	for _, m := range []map[uint]*ingestCancelEntry{w.recorderCancels, w.asrCancels, w.finalizeCancels} {
		if entry, ok := m[materialID]; ok && entry != nil {
			cancels = append(cancels, entry.cancel)
			delete(m, materialID)
		}
	}
	w.cancelsMu.Unlock()
	if len(cancels) == 0 {
		return false
	}
	for _, c := range cancels {
		c()
	}
	w.logger.Info("已请求取消跟播任务", zap.Uint("material_id", materialID))
	return true
}

func (w *liveIngestWorker) startHeartbeat(ctx context.Context, id uint, epoch int64) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(liveIngestHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				_ = w.repo.HeartbeatIngest(ctx, id, epoch)
			}
		}
	}()
	return func() { close(done) }
}

func (w *liveIngestWorker) startASRHeartbeat(ctx context.Context, id uint, asrEpoch int64) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(liveIngestHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				_ = w.repo.HeartbeatASR(ctx, id, asrEpoch)
			}
		}
	}()
	return func() { close(done) }
}
