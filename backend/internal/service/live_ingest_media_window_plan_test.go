package service

import (
	"testing"
)

func TestPlanMediaWindowSeal_DoesNotStallJustUnderWindow(t *testing.T) {
	// 真实录像里每片约 6057ms：99 片 ≈ 599379 < 600000，再加一片会略超窗。
	// 旧逻辑在「将要超过」时 break 且不纳入该片，导致永久 partial、永不封窗。
	durs := map[int64]int64{}
	for i := int64(0); i < 120; i++ {
		durs[i] = 6057
	}
	segEnd, sumMS, partial := planMediaWindowSeal(0, 120, durs, 600000, 6000, false)
	if partial {
		t.Fatalf("partial=%v sumMS=%d segEnd=%d, want seal-ready", partial, sumMS, segEnd)
	}
	if sumMS < 600000 {
		t.Fatalf("sumMS=%d want >= 600000", sumMS)
	}
	if segEnd != 100 {
		t.Fatalf("segEnd=%d want 100 (99*6057 under + 1 overshoot)", segEnd)
	}
}

func TestPlanMediaWindowSeal_WaitsWhenFarFromFull(t *testing.T) {
	durs := map[int64]int64{}
	for i := int64(0); i < 50; i++ {
		durs[i] = 6057
	}
	_, sumMS, partial := planMediaWindowSeal(0, 50, durs, 600000, 6000, false)
	if !partial {
		t.Fatalf("expected partial, sumMS=%d", sumMS)
	}
	if sumMS >= 600000 {
		t.Fatalf("sumMS=%d unexpectedly full", sumMS)
	}
}
