package model

import "testing"

func TestLiveMaterial_PlayURL(t *testing.T) {
	m := &LiveMaterial{
		LiveURL:           "https://cdn.example/master.mp4",
		M3U8URL:           "https://src.example/live.m3u8",
		RecordPlaylistURL: "https://cdn.example/live.m3u8",
		URLType:           URLTypeM3U8,
		LiveStatus:        LiveStatusLive,
	}
	if got := m.PlayURL(); got != "" {
		t.Errorf("live without sealed windows PlayURL = %q, want empty (preallocated live_url ignored)", got)
	}

	m.MediaWindows = MediaWindowList{{
		Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true,
		URL: "https://cdn.example/windows/window_00000.mp4",
	}}.Marshal()
	m.Duration = 600000
	if got := m.PlayURL(); got != "https://cdn.example/master.mp4" {
		t.Errorf("live with master PlayURL = %q, want live_url master", got)
	}

	m.LiveURL = ""
	if got := m.PlayURL(); got != "https://cdn.example/windows/window_00000.mp4" {
		t.Errorf("single window fallback PlayURL = %q", got)
	}

	m.LiveURL = "https://cdn.example/master.mp4"
	m.MediaWindows = MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true, URL: "https://cdn.example/w0.mp4"},
		{Index: 1, StartMS: 600000, EndMS: 1200000, DurMS: 600000, Ready: true, URL: "https://cdn.example/w1.mp4"},
	}.Marshal()
	m.Duration = 1200000
	if got := m.PlayURL(); got != "https://cdn.example/master.mp4" {
		t.Errorf("multi-window PlayURL = %q, want concat master", got)
	}

	m.MediaWindows = "[]"
	m.URLType = URLTypeFile
	m.LiveStatus = LiveStatusEnded
	m.Duration = 1200000
	if got := m.PlayURL(); got != m.LiveURL {
		t.Errorf("ended PlayURL = %q, want live_url", got)
	}
	replay := &LiveMaterial{M3U8URL: "https://src.example/vod.m3u8", URLType: URLTypeM3U8, LiveStatus: LiveStatusNone}
	if got := replay.PlayURL(); got != replay.M3U8URL {
		t.Errorf("replay PlayURL = %q", got)
	}
}

func TestLiveMaterial_ASRCoversClips(t *testing.T) {
	m := &LiveMaterial{
		SourceMode:  SourceModeLive,
		LiveStatus:  LiveStatusLive,
		ASRStatus:   ASRStatusProcessing,
		ASRCursorMS: 120000,
	}
	if !m.ASRCoversClips([]ClipRange{{StartTime: 0, EndTime: 60000}}) {
		t.Fatal("expected cursor to cover clip")
	}
	if m.ASRCoversClips([]ClipRange{{StartTime: 0, EndTime: 180000}}) {
		t.Fatal("clip beyond cursor should not be covered")
	}
	done := &LiveMaterial{ASRStatus: ASRStatusCompleted}
	if !done.ASRCoversClips(nil) {
		t.Fatal("completed ASR should cover any clips")
	}
}

func TestFormatClockMS(t *testing.T) {
	if got := FormatClockMS(0); got != "0:00" {
		t.Errorf("FormatClockMS(0) = %q", got)
	}
	if got := FormatClockMS(3312_000); got != "55:12" {
		t.Errorf("FormatClockMS(3312000) = %q", got)
	}
	if got := FormatClockMS(3723_000); got != "1:02:03" {
		t.Errorf("FormatClockMS(3723000) = %q", got)
	}
}
