package service

import (
	"context"
	"errors"
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

func TestEnqueueWake_FillsUpToCapacity(t *testing.T) {
	ch := newWakeChan(3)
	enqueueWake(ch, 3)
	enqueueWake(ch, 3)
	if len(ch) != 3 {
		t.Fatalf("len = %d, want 3", len(ch))
	}
}
