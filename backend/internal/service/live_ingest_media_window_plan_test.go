package service

import (
	"testing"

	"live-mixer/internal/model"
)

func TestNextWindowIndex(t *testing.T) {
	m := &model.LiveMaterial{MediaWindows: "[]", NextWindowSeg: 0, NextSeg: 0}
	if got := nextWindowIndex(m); got != 0 {
		t.Fatalf("empty = %d, want 0", got)
	}

	m.MediaWindows = model.MediaWindowList{
		{Index: 0, DurMS: 600000, EndMS: 600000, Ready: true},
	}.Marshal()
	m.NextWindowSeg = 1
	m.NextSeg = 1
	if got := nextWindowIndex(m); got != 1 {
		t.Fatalf("one ready = %d, want 1", got)
	}

	// DB 游标超前时以游标为准，避免覆盖已登记窗。
	m.NextWindowSeg = 3
	if got := nextWindowIndex(m); got != 3 {
		t.Fatalf("cursor ahead = %d, want 3", got)
	}
}

func TestMinRecordedWindowMS(t *testing.T) {
	if minRecordedWindowMS < 1000 {
		t.Fatalf("minRecordedWindowMS=%d, want >= 1000", minRecordedWindowMS)
	}
}
