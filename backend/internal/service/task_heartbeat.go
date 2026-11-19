package service

import (
	"context"
	"sync"
	"time"

	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

// taskHeartbeatInterval 长耗时外部调用（大模型生成）期间的默认心跳间隔。
// 必须远小于孤儿回收阈值（AI 切片默认 20 分钟）：1 分钟可在窗口内刷新约 20 次。
const taskHeartbeatInterval = time.Minute

// startTaskHeartbeat 按 interval 周期性刷新任务的 updated_at（进度保持不变），返回幂等的停止函数；
// 调用方必须在外部调用返回后立刻停止。
//
// 为什么需要：大模型调用期间没有任何写库，一旦耗时超过 RequeueStaleProcessingByType 的阈值，
// 仍在正常生成的任务会被判成孤儿、改回 pending 并被另一个 Worker 再跑一遍（重复调用大模型、重复计费）。
// 写库用 dbWriteCtx（Background）：taskCtx 超时不影响心跳，也不会把 taskCtx 取消传进 DB。
// 租约失效（version 已被回收逻辑递增）或 ctx 取消时自行退出；写库失败只告警，不影响任务本身。
func startTaskHeartbeat(
	ctx context.Context,
	repo repository.TaskRepository,
	logger *zap.Logger,
	taskID string,
	version int64,
	progress int16,
	interval time.Duration,
) func() {
	if repo == nil || taskID == "" || interval <= 0 {
		return func() {}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	writeCtx := dbWriteCtx(ctx)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				ok, err := repo.HeartbeatProgress(writeCtx, taskID, version, progress)
				if err != nil {
					logger.Warn("任务心跳写库失败",
						zap.String("task_id", taskID),
						zap.Error(err),
					)
					continue
				}
				if !ok {
					logger.Warn("任务心跳租约已失效，已停止心跳（任务可能已被回收重排）",
						zap.String("task_id", taskID),
						zap.Int64("version", version),
					)
					return
				}
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}
