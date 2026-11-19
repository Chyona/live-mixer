package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRunClaimedWork_RecoversPanic(t *testing.T) {
	var failed error
	runClaimedWork(context.Background(), zap.NewNop(), "test", 0, "t1", time.Minute,
		func(context.Context) error {
			panic("boom")
		},
		func(_ context.Context, err error) {
			failed = err
		},
	)
	if failed == nil || !strings.Contains(failed.Error(), "boom") {
		t.Fatalf("markFailed = %v, want panic error", failed)
	}
}

func TestRunClaimedWork_TimeoutMarksFailed(t *testing.T) {
	var failed error
	runClaimedWork(context.Background(), zap.NewNop(), "test", 0, "t1", 20*time.Millisecond,
		func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
		func(_ context.Context, err error) {
			failed = err
		},
	)
	if failed == nil {
		t.Fatal("expected timeout markFailed")
	}
	if !errors.Is(failed, context.DeadlineExceeded) && !strings.Contains(failed.Error(), "超时") {
		t.Fatalf("markFailed = %v, want timeout", failed)
	}
}

func TestRunClaimedWork_TimeoutMarksFailedEvenIfErrorNotWrapped(t *testing.T) {
	var failed error
	runClaimedWork(context.Background(), zap.NewNop(), "test", 0, "t1", 20*time.Millisecond,
		func(ctx context.Context) error {
			<-ctx.Done()
			// 模拟下载层用 %v 丢掉了 context 包装。
			return fmt.Errorf("write file failed: %v", ctx.Err())
		},
		func(_ context.Context, err error) {
			failed = err
		},
	)
	if failed == nil {
		t.Fatal("expected timeout markFailed even when error is not wrapped")
	}
}

func TestRunClaimedWork_SuccessDoesNotMarkFailed(t *testing.T) {
	called := false
	runClaimedWork(context.Background(), zap.NewNop(), "test", 0, "t1", time.Minute,
		func(context.Context) error { return nil },
		func(context.Context, error) { called = true },
	)
	if called {
		t.Fatal("markFailed should not be called on success")
	}
}

func TestProcessHardTimeout(t *testing.T) {
	if got := processHardTimeout(20 * time.Millisecond); got != 20*time.Millisecond {
		t.Fatalf("short timeout = %v, want passthrough", got)
	}
	if got := processHardTimeout(90 * time.Minute); got != 6*time.Hour {
		t.Fatalf("90m stale → hard = %v, want 6h", got)
	}
	if got := processHardTimeout(2 * time.Hour); got != 8*time.Hour {
		t.Fatalf("2h stale → hard = %v, want 8h", got)
	}
}

func TestDbWriteCtxIgnoresParentCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	write := dbWriteCtx(parent)
	if write.Err() != nil {
		t.Fatalf("dbWriteCtx should not be canceled, err=%v", write.Err())
	}
}

func TestEnqueueWake_FillsUpToCapacity(t *testing.T) {
	ch := newWakeChan(3)
	enqueueWake(ch, 3)
	enqueueWake(ch, 3)
	if len(ch) != 3 {
		t.Fatalf("len = %d, want 3", len(ch))
	}
}

// TestRunClaimedWork_InnerDeadlineIsNotReportedAsTaskTimeout 验证「子调用自己超时」不再被冒名成「任务执行超时」。
// 线上 AI 切片用的就是这个形状：LLM 客户端 30 分钟预算用尽，错误解包出 context.DeadlineExceeded，
// 但任务级 taskCtx 其实还活着（硬超时 6 小时）。旧文案把两者混为一谈，排查方向直接跑偏。
func TestRunClaimedWork_InnerDeadlineIsNotReportedAsTaskTimeout(t *testing.T) {
	var failed error
	runClaimedWork(context.Background(), zap.NewNop(), "test", 0, "t1", time.Minute,
		func(context.Context) error {
			return fmt.Errorf("调用大模型失败(耗时 30m0s): %w", context.DeadlineExceeded)
		},
		func(_ context.Context, err error) {
			failed = err
		},
	)
	if failed == nil {
		t.Fatal("expected markFailed")
	}
	msg := failed.Error()
	if strings.Contains(msg, "任务执行超时") {
		t.Errorf("error = %q, 子调用超时不应报成任务超时", msg)
	}
	if !strings.Contains(msg, "任务执行失败") {
		t.Errorf("error = %q, want 含「任务执行失败」", msg)
	}
	// 仍然是超时失败：ASR 重试与前端展示都依赖这个判定。
	if !errors.Is(failed, context.DeadlineExceeded) {
		t.Errorf("errors.Is(DeadlineExceeded) = false, err = %v", failed)
	}
}

// TestRunClaimedWork_TaskDeadlineStillSaysTimeout 验证任务级硬超时才用「任务执行超时」文案。
func TestRunClaimedWork_TaskDeadlineStillSaysTimeout(t *testing.T) {
	var failed error
	runClaimedWork(context.Background(), zap.NewNop(), "test", 0, "t1", 20*time.Millisecond,
		func(ctx context.Context) error {
			<-ctx.Done()
			return fmt.Errorf("下载中断: %w", ctx.Err())
		},
		func(_ context.Context, err error) {
			failed = err
		},
	)
	if failed == nil {
		t.Fatal("expected markFailed")
	}
	if !strings.Contains(failed.Error(), "任务执行超时") {
		t.Errorf("error = %q, want 含「任务执行超时」", failed)
	}
}
