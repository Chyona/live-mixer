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
	ended := BuildEventPlaylist([]PlaylistItem{{URL: "https://cdn.example/seg_00000.ts"}}, 6, true)
	if !strings.Contains(ended, "#EXT-X-ENDLIST") {
		t.Fatal("ended playlist should have ENDLIST")
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
