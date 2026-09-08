package storage

import "testing"

func TestObjectContentType(t *testing.T) {
	if got := objectContentType("video_editing/live-record/u/live.m3u8"); got != "application/vnd.apple.mpegurl" {
		t.Errorf("m3u8 = %q", got)
	}
	if got := objectContentType("seg/1/seg_00000.ts"); got != "video/MP2T" {
		t.Errorf("ts = %q", got)
	}
	if got := objectContentType("final.mp4"); got != "" {
		t.Errorf("mp4 should leave default, got %q", got)
	}
}
