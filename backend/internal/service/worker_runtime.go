package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

func newWakeChan(concurrency int) chan struct{} {
	n := concurrency
	if n <= 0 {
		n = 1
	}
	return make(chan struct{}, n)
}

// enqueueWake 向容量为 N 的 wake channel 尽量填满信号，唤醒所有空闲 Worker。
// 非阻塞：通道已满时立即返回，避免创建任务的请求被调度逻辑拖住。
func enqueueWake(wake chan struct{}, n int) {
	if wake == nil {
		return
	}
	if n <= 0 {
		n = 1
	}
	for i := 0; i < n; i++ {
		select {
		case wake <- struct{}{}:
		default:
			return
		}
	}
}

// runClaimedWork 执行已抢占的工作：单任务超时 + panic 恢复。
//
// 旧实现里 Process 使用进程级 ctx，且 drain 无 recover。ffmpeg/下载一旦挂起或 panic，
// 该 Worker 槽位永久占用；RequeueStale 只把 DB 改回 pending，goroutine 仍卡在 Process 里，
// 其余 pending 任务会一直待处理。一键成片默认并发为 3，这与「同批 10 条里卡住 3 条」吻合。
func runClaimedWork(
	ctx context.Context,
	logger *zap.Logger,
	workerName string,
	workerID int,
	taskID string,
	staleTimeout time.Duration,
	process func(context.Context) error,
	markFailed func(context.Context, error),
) {
	if logger == nil {
		logger = zap.NewNop()
	}

	defer func() {
		if rec := recover(); rec != nil {
			logger.Error("Worker 执行 panic，已恢复以免槽位泄漏",
				zap.String("worker", workerName),
				zap.Int("worker_id", workerID),
				zap.String("task_id", taskID),
				zap.Any("panic", rec),
				zap.Stack("stack"),
			)
			if markFailed != nil && ctx.Err() == nil {
				markFailed(ctx, fmt.Errorf("任务执行异常: %v", rec))
			}
		}
	}()

	taskCtx := ctx
	cancel := func() {}
	if staleTimeout > 0 {
		taskCtx, cancel = context.WithTimeout(ctx, staleTimeout)
	}
	defer cancel()

	err := process(taskCtx)
	if err == nil {
		return
	}
	logger.Error("任务执行失败",
		zap.String("worker", workerName),
		zap.Int("worker_id", workerID),
		zap.String("task_id", taskID),
		zap.Error(err),
	)
	if ctx.Err() != nil || markFailed == nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		markFailed(ctx, fmt.Errorf("任务执行超时: %w", err))
	}
}
