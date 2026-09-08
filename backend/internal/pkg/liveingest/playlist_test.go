package liveingest

import (
	"strings"
	"testing"
)

func TestBuildEventPlaylist(t *testing.T) {
	body := BuildEventPlaylist([]PlaylistItem{
		{DurationSec: 6, URL: "https://cdn.example/seg_00000.ts"},
		{DurationSec: 6, URL: "https://cdn.example/seg_00001.ts"},
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
	if strings.Count(body, "#EXT-X-DISCONTINUITY") != 1 {
		t.Fatalf("want 1 discontinuity between 2 items, got %s", body)
	}
	ended := BuildEventPlaylist([]PlaylistItem{{URL: "https://cdn.example/seg_00000.ts"}}, 6, true)
	if !strings.Contains(ended, "#EXT-X-ENDLIST") {
		t.Fatal("ended playlist should have ENDLIST")
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
}
