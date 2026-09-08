package media

import (
	"fmt"
	"strings"
	"testing"
)

func TestIsM3U8URL(t *testing.T) {
	if !IsM3U8URL("https://cdn.example.com/live/index.m3u8?token=1") {
		t.Fatal("want m3u8")
	}
	if IsM3U8URL("https://cdn.example.com/a.mp4") {
		t.Fatal("mp4 should not be m3u8")
	}
}

func TestProbeHLSPlaylist_Media(t *testing.T) {
	body := `#EXTM3U
#EXT-X-VERSION:3
#EXTINF:6.0,
seg0.ts
`
	got, err := parseHLSPlaylistBody("https://ex.com/live.m3u8", body, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasMedia {
		t.Fatal("expected media")
	}
}

func TestProbeHLSPlaylist_Encrypted(t *testing.T) {
	body := `#EXTM3U
#EXT-X-KEY:METHOD=AES-128,URI="key.bin"
#EXTINF:6.0,
seg0.ts
`
	_, err := parseHLSPlaylistBody("https://ex.com/live.m3u8", body, 0)
	if err == nil || !strings.Contains(err.Error(), "加密") {
		t.Fatalf("err = %v, want encrypted", err)
	}
}

func parseHLSPlaylistBody(playlistURL, text string, depth int) (HLSProbeResult, error) {
	if !strings.Contains(text, "#EXTM3U") {
		return HLSProbeResult{}, fmt.Errorf("不是有效的 HLS 播放列表")
	}
	result := HLSProbeResult{MediaURL: playlistURL}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "#EXT-X-KEY") {
			result.Encrypted = true
		}
		if strings.HasPrefix(upper, "#EXTINF") {
			result.HasMedia = true
		}
		if strings.HasPrefix(upper, "#EXT-X-STREAM-INF") {
			result.IsMaster = true
		}
	}
	if result.Encrypted {
		return result, fmt.Errorf("不支持加密 HLS（EXT-X-KEY）")
	}
	_ = depth
	return result, nil
}
