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

func TestHLSInputArgs_LocalM3U8AllowsHTTPS(t *testing.T) {
	args := HLSInputArgs(`docker\html\staging\job\source_vod.m3u8`)
	if len(args) < 2 || args[0] != "-protocol_whitelist" {
		t.Fatalf("local m3u8 must set protocol_whitelist, got %v", args)
	}
	if !strings.Contains(args[1], "https") {
		t.Fatalf("whitelist missing https: %v", args)
	}
	// 本地清单本身非 HTTP，不必强加 reconnect（分片拉流由 hls demuxer 处理）。
	for _, a := range args {
		if a == "-reconnect" {
			t.Fatalf("unexpected reconnect on local m3u8: %v", args)
		}
	}
}

func TestHLSInputArgs_HTTPURLIncludesReconnect(t *testing.T) {
	args := HLSInputArgs("https://cdn.example.com/live.m3u8")
	found := false
	for _, a := range args {
		if a == "-reconnect" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("http m3u8 should include reconnect: %v", args)
	}
}

func TestHLSInputArgs_LocalMP4Nil(t *testing.T) {
	if got := HLSInputArgs("/tmp/source.mp4"); got != nil {
		t.Fatalf("local mp4 should not add HLS args, got %v", got)
	}
}

func TestConcatDemuxerInputArgs(t *testing.T) {
	args := ConcatDemuxerInputArgs(`E:\staging\source_local.ffconcat`)
	if len(args) != 3 || args[0] != "-f" || args[1] != "concat" || args[2] != "-safe" {
		// -safe 0 is two tokens: "-safe", "0"
	}
	if len(args) != 4 || args[0] != "-f" || args[1] != "concat" || args[2] != "-safe" || args[3] != "0" {
		t.Fatalf("got %v", args)
	}
	if ConcatDemuxerInputArgs("a.mp4") != nil {
		t.Fatal("mp4")
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
