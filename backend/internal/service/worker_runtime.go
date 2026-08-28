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

	// 写库必须用未取消的 context：taskCtx 超时后 GORM 写入会立刻失败，任务会卡在 processing。
	writeCtx := dbWriteCtx(ctx)

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
				markFailed(writeCtx, fmt.Errorf("任务执行异常: %v", rec))
			}
		}
	}()

	taskCtx := ctx
	cancel := func() {}
	if staleTimeout > 0 {
		// staleTimeout 用于 RequeueStale（无心跳孤儿回收）。
		// 硬超时需远大于 stale：19GB 直播源在带宽争抢下经常超过 90 分钟；
		// 若硬超时=stale，会误杀仍在下载的任务；若此时 MarkFailed 写库失败，就会永久卡在 processing。
		taskCtx, cancel = context.WithTimeout(ctx, processHardTimeout(staleTimeout))
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
	// 任务级超时/取消时，无论错误是否用 %w 包装了 context，都要落库失败状态。
	if taskCtx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		markFailed(writeCtx, fmt.Errorf("任务执行超时: %w", err))
	}
}

// processHardTimeout 将「孤儿回收阈值」映射为单任务硬超时。
// 短于 1 分钟的值原样返回（单测）；生产路径至少 6 小时，避免大文件下载被误杀。
func processHardTimeout(staleTimeout time.Duration) time.Duration {
	if staleTimeout < time.Minute {
		return staleTimeout
	}
	hard := 6 * time.Hour
	if 4*staleTimeout > hard {
		hard = 4 * staleTimeout
	}
	return hard
}

// dbWriteCtx 返回可安全写库的 context：使用 Background，彻底脱离 taskCtx 的取消/超时，
// 避免「下载超时后 MarkFailed 也因 context deadline exceeded 写不进库 → 任务永久卡在 processing」。
func dbWriteCtx(ctx context.Context) context.Context {
	_ = ctx
	return context.Background()
}
