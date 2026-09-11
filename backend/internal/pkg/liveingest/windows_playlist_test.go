package liveingest

import (
	"strings"
	"testing"

	"live-mixer/internal/model"
)

func TestBuildWindowsPlaylist(t *testing.T) {
	body := BuildWindowsPlaylist(model.MediaWindowList{
		{Index: 0, DurMS: 600000, Ready: true, TSURL: "https://cdn/w0.ts"},
		{Index: 1, DurMS: 300000, Ready: true, TSURL: "https://cdn/w1.ts"},
		{Index: 2, DurMS: 100, Ready: false, TSURL: "https://cdn/w2.ts"},
	}, 600, true)
	if !strings.Contains(body, "#EXT-X-ENDLIST") {
		t.Fatalf("missing ENDLIST: %s", body)
	}
	if !strings.Contains(body, "w0.ts") || !strings.Contains(body, "w1.ts") {
		t.Fatalf("missing urls: %s", body)
	}
	if strings.Contains(body, "w2.ts") {
		t.Fatalf("included not-ready window: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-DISCONTINUITY") {
		t.Fatalf("missing DISCONTINUITY between windows: %s", body)
	}
}
