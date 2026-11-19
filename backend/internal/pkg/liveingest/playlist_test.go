package liveingest

import (
	"strings"
	"testing"
)

func TestBuildEventPlaylist(t *testing.T) {
	body := BuildEventPlaylist([]PlaylistItem{
		{DurationSec: 6, URL: "https://cdn.example/live-record/u/seg/1/seg_00000.ts"},
		{DurationSec: 6, URL: "https://cdn.example/live-record/u/seg/1/seg_00001.ts"},
	}, 6, false)
	if !strings.Contains(body, "#EXT-X-PLAYLIST-TYPE:EVENT") {
		t.Fatalf("missing EVENT type in %s", body)
	}
	if strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatal("live playlist should not have ENDLIST")
	}
	if !strings.Contains(body, "#EXT-X-INDEPENDENT-SEGMENTS") {
		t.Fatal("missing INDEPENDENT-SEGMENTS")
	}
	if strings.Contains(body, "#EXT-X-DISCONTINUITY") {
		t.Fatalf("same-epoch segments should not have discontinuity: %s", body)
	}
	ended := BuildEventPlaylist([]PlaylistItem{{URL: "https://cdn.example/seg_00000.ts"}}, 6, true)
	if !strings.Contains(ended, "#EXT-X-ENDLIST") {
		t.Fatal("ended playlist should have ENDLIST")
	}

	resume := BuildEventPlaylist([]PlaylistItem{
		{DurationSec: 6, URL: "https://cdn.example/live-record/u/seg/1/seg_00009.ts"},
		{DurationSec: 6, URL: "https://cdn.example/live-record/u/seg/2/seg_00010.ts"},
	}, 6, false)
	if strings.Count(resume, "#EXT-X-DISCONTINUITY") != 1 {
		t.Fatalf("want 1 discontinuity at epoch change, got %s", resume)
	}
}

func TestPreferStablePlaylistURL(t *testing.T) {
	existing := "https://cdn.example/live.m3u8?sig=old"
	if got := PreferStablePlaylistURL(existing, "https://cdn.example/live.m3u8?sig=new"); got != existing {
		t.Fatalf("PreferStablePlaylistURL = %q, want existing", got)
	}
	if got := PreferStablePlaylistURL("", "https://cdn.example/x.m3u8"); got != "https://cdn.example/x.m3u8" {
		t.Fatalf("PreferStablePlaylistURL empty existing = %q", got)
	}
}

func TestSealPlaylistAsVOD(t *testing.T) {
	live := BuildEventPlaylist([]PlaylistItem{{DurationSec: 6, URL: "https://cdn.example/seg_00000.ts"}}, 6, false)
	sealed := SealPlaylistAsVOD(live)
	if !strings.Contains(sealed, "#EXT-X-ENDLIST") {
		t.Fatalf("sealed missing ENDLIST: %s", sealed)
	}
	if !strings.Contains(sealed, "seg_00000.ts") {
		t.Fatalf("sealed lost segment url: %s", sealed)
	}
	already := SealPlaylistAsVOD(sealed)
	if strings.Count(already, "#EXT-X-ENDLIST") != 1 {
		t.Fatalf("want single ENDLIST, got %s", already)
	}
}

func TestWindowStartIndex(t *testing.T) {
	if got := WindowStartIndex(0, 6); got != 0 {
		t.Errorf("got %d", got)
	}
	if got := WindowStartIndex(18000, 6); got != 3 {
		t.Errorf("got %d want 3", got)
	}
	if got := WindowStartIndex(185000, 6); got != 30 {
		t.Errorf("got %d want 30", got)
	}
}

func TestWindowOffsetMS(t *testing.T) {
	if got := WindowOffsetMS(0, 6); got != 0 {
		t.Errorf("aligned cursor 0: got %d", got)
	}
	if got := WindowOffsetMS(18000, 6); got != 18000 {
		t.Errorf("exact boundary: got %d want 18000", got)
	}
	// cursor 落在分片中间时，偏移必须回到分片起点，不能用裸 cursor。
	if got := WindowOffsetMS(185000, 6); got != 180000 {
		t.Errorf("mid-segment cursor: got %d want 180000", got)
	}
	if got := WindowOffsetMS(185000, 6); got == 185000 {
		t.Fatal("offset must not equal raw mid-segment cursor")
	}
}

func TestResolveWindowStartByDurations_NonNominalSegs(t *testing.T) {
	// 101 片 × 5900ms ≈ 595900；若误用 cursor/6000 会得到 startSeg=99，实测应约为 100。
	durs := make([]int64, 200)
	for i := range durs {
		durs[i] = 5900
	}
	cursor := int64(101 * 5900) // 第一窗结束后的真实游标
	start, offset := ResolveWindowStartByDurations(durs, cursor, 0)
	if start != 101 {
		t.Fatalf("startSeg=%d want 101 (nominal would be %d)", start, cursor/6000)
	}
	if offset != cursor {
		t.Fatalf("offset=%d want %d", offset, cursor)
	}
	// overlap 1 片
	start2, offset2 := ResolveWindowStartByDurations(durs, cursor, 1)
	if start2 != 100 {
		t.Fatalf("overlap startSeg=%d want 100", start2)
	}
	if offset2 != 100*5900 {
		t.Fatalf("overlap offset=%d want %d", offset2, 100*5900)
	}
}

func TestResolveWindowStartByDurations_MidSegment(t *testing.T) {
	durs := []int64{6000, 6000, 6000, 6000}
	start, offset := ResolveWindowStartByDurations(durs, 15000, 0)
	if start != 2 || offset != 12000 {
		t.Fatalf("got start=%d offset=%d", start, offset)
	}
}

func TestSumSegmentDurationsMS(t *testing.T) {
	durs := []int64{1000, 2000, 3000, 4000}
	if got := SumSegmentDurationsMS(durs, 1, 2); got != 5000 {
		t.Fatalf("got %d", got)
	}
}
