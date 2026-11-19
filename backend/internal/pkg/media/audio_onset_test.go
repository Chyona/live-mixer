package media

import "testing"

func TestOnsetShiftMS_DetectsDelay(t *testing.T) {
	sr := 16000
	n := sr / 2 // 500ms
	ref := make([]int16, n)
	for i := range ref {
		// 简单脉冲：在 100ms 处
		if i == sr/10 {
			ref[i] = 20000
		}
	}
	delaySamples := sr * 50 / 1000 // 50ms
	clip := make([]int16, n)
	copy(clip[delaySamples:], ref[:n-delaySamples])

	shift, ok := OnsetShiftMS(ref, clip, sr, 120)
	if !ok {
		t.Fatal("expected ok")
	}
	if shift < 40 || shift > 60 {
		t.Fatalf("shift=%d want ~50", shift)
	}
}
