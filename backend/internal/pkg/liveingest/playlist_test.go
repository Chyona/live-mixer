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
	uploaded := "https://cdn.example/live.m3u8?sig=new"
	if got := PreferStablePlaylistURL(existing, uploaded); got != existing {
		t.Errorf("got %s, want existing", got)
	}
	if got := PreferStablePlaylistURL("  ", uploaded); got != uploaded {
		t.Errorf("got %s, want uploaded", got)
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
