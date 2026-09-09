package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/webroot"

	"go.uber.org/zap"
)

func TestLiveIngestWorker_Cancel_NoRunningTask(t *testing.T) {
	w := NewLiveIngestWorker(nil, nil, nil, nil, nil, webroot.Config{}, zap.NewNop(), 1)
	if w.Cancel(99) {
		t.Fatal("Cancel() = true, want false when no running task")
	}
}

func TestLiveIngestWorker_Cancel_StopsWaitingProcess(t *testing.T) {
	raw := NewLiveIngestWorker(nil, nil, nil, nil, nil, webroot.Config{}, zap.NewNop(), 1)
	w, ok := raw.(*liveIngestWorker)
	if !ok {
		t.Fatal("NewLiveIngestWorker should return *liveIngestWorker")
	}

	scheduled := time.Now().Add(time.Hour)
	material := &model.LiveMaterial{
		ID:          42,
		LiveStatus:  model.LiveStatusWaiting,
		SourceMode:  model.SourceModeLive,
		IngestEpoch: 1,
		ScheduledAt: &scheduled,
		M3U8URL:     "https://example.com/live.m3u8",
	}

	done := make(chan error, 1)
	go func() {
		done <- w.Process(context.Background(), material)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		w.cancelsMu.Lock()
		_, registered := w.cancels[material.ID]
		w.cancelsMu.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for cancel registration")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !w.Cancel(material.ID) {
		t.Fatal("Cancel() = false, want true")
	}

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Process() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Process did not exit after Cancel")
	}

	if w.Cancel(material.ID) {
		t.Fatal("second Cancel() should be false")
	}
}
