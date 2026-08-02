package asr

import "testing"

func TestBuildSubtitleTXT(t *testing.T) {
	got := BuildSubtitleTXT(
		[]string{"财富", " 直播 ", "", "老师"},
		[]Utterance{
			{Speaker: "1", StartTime: 2000, Text: "那啥数字？"},
			{Speaker: "2", StartTime: 4000, Text: "热。"},
			{Speaker: "2", StartTime: 4500, Text: "热度，直播间热度。"},
			{Speaker: "1", StartTime: 7000, Text: "嗯，右边那个呢？"},
		},
	)
	want := "关键词\n财富 直播 老师\n\n文字记录\n说话人1 00:02\n那啥数字？\n\n说话人2 00:04\n热。热度，直播间热度。\n\n说话人1 00:07\n嗯，右边那个呢？\n"
	if got != want {
		t.Fatalf("BuildSubtitleTXT() =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatASRClock(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "00:00"},
		{2000, "00:02"},
		{65_000, "01:05"},
		{3_661_000, "1:01:01"},
	}
	for _, tt := range tests {
		if got := formatASRClock(tt.ms); got != tt.want {
			t.Errorf("formatASRClock(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}
