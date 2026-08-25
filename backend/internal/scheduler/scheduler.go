// Package scheduler 提供与业务 Worker 解耦的全局定时任务调度。
package scheduler

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Job 周期性任务定义。
type Job struct {
	// Name 任务名，用于日志。
	Name string
	// Interval 执行间隔；<=0 时该 Job 不会被调度。
	Interval time.Duration
	// Run 执行体；应尽快返回，避免阻塞同 Job 的下一轮 ticker。
	Run func(ctx context.Context)
}

// Scheduler 全局定时调度器：每个 Job 独立 ticker，启动时立即执行一次。
type Scheduler struct {
	logger *zap.Logger
	jobs   []Job

	startOnce sync.Once
}

// New 创建调度器；logger 为 nil 时使用 Nop。
func New(logger *zap.Logger) *Scheduler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Scheduler{logger: logger}
}

// Register 注册定时任务；须在 Start 之前调用。
func (s *Scheduler) Register(job Job) {
	s.jobs = append(s.jobs, job)
}

// Start 为每个合法 Job 启动独立循环；可安全重复调用（仅首次生效）。
func (s *Scheduler) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		for _, job := range s.jobs {
			if job.Interval <= 0 || job.Run == nil {
				s.logger.Warn("跳过无效定时任务",
					zap.String("job", job.Name),
					zap.Duration("interval", job.Interval),
				)
				continue
			}
			j := job
			go s.loop(ctx, j)
			s.logger.Info("定时任务已启动",
				zap.String("job", j.Name),
				zap.Duration("interval", j.Interval),
			)
		}
	})
}

func (s *Scheduler) loop(ctx context.Context, job Job) {
	s.runSafe(ctx, job)

	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runSafe(ctx, job)
		}
	}
}

func (s *Scheduler) runSafe(ctx context.Context, job Job) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("定时任务 panic",
				zap.String("job", job.Name),
				zap.Any("recover", rec),
			)
		}
	}()
	job.Run(ctx)
}
