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
		t.Errorf("live without master ready PlayURL = %q, want empty", got)
	}
	m.MediaWindows = WithMaster(MediaWindow{
		StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true,
		URL: "https://cdn.example/master.mp4?signed=1",
	}).Marshal()
	if got := m.PlayURL(); got != "https://cdn.example/master.mp4?signed=1" {
		t.Errorf("live with master PlayURL = %q", got)
	}
	m.MediaWindows = "[]"
	m.URLType = URLTypeFile
	m.LiveStatus = LiveStatusEnded
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
