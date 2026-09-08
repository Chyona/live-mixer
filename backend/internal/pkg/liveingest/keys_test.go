package liveingest

import "testing"

func TestSegmentStorageEpoch(t *testing.T) {
	if got := SegmentStorageEpoch(2, 625, 0); got != 1 {
		t.Errorf("old segment after reclaim = %d, want 1", got)
	}
	if got := SegmentStorageEpoch(2, 625, 624); got != 1 {
		t.Errorf("last old segment = %d, want 1", got)
	}
	if got := SegmentStorageEpoch(2, 625, 625); got != 2 {
		t.Errorf("new segment after reclaim = %d, want 2", got)
	}
	if got := SegmentStorageEpoch(1, 0, 3); got != 1 {
		t.Errorf("first session = %d, want 1", got)
	}
}
