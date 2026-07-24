package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestScheduler_RunsImmediatelyAndOnInterval(t *testing.T) {
	var n atomic.Int32
	s := New(zap.NewNop())
	s.Register(Job{
		Name:     "counter",
		Interval: 40 * time.Millisecond,
		Run: func(ctx context.Context) {
			n.Add(1)
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if n.Load() >= 2 {
			cancel()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("count = %d, want >= 2", n.Load())
}

func TestScheduler_SkipsInvalidJobs(t *testing.T) {
	s := New(zap.NewNop())
	s.Register(Job{Name: "no-interval", Interval: 0, Run: func(context.Context) {}})
	s.Register(Job{Name: "no-run", Interval: time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	// 不应 panic；给一点时间确认无后台循环误启动
	time.Sleep(20 * time.Millisecond)
}
