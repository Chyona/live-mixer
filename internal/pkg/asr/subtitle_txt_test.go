package asr

import "testing"

func TestBuildSubtitleTXT(t *testing.T) {
	got := BuildSubtitleTXT(
		[]string{"财富", " 直播 ", "", "老师"},
		[]SubtitleParagraph{
			{Speaker: "1", StartTime: 2000, Text: "那啥数字？"},
			{Speaker: "2", StartTime: 4000, Text: "热。热度，直播间热度。"},
			{Speaker: "1", StartTime: 7000, Text: "嗯，右边那个呢？"},
		},
	)
	want := "关键词\n财富 直播 老师\n\n文字记录\n说话人1 00:02\n那啥数字？\n\n说话人2 00:04\n热。热度，直播间热度。\n\n说话人1 00:07\n嗯，右边那个呢？\n"
	if got != want {
		t.Fatalf("BuildSubtitleTXT() =\n%q\nwant\n%q", got, want)
	}
}

func TestBuildSubtitleTXT_MatchesParagraphsExactly(t *testing.T) {
	// 即使相邻同说话人，也不合并——与 asr_paragraphs 分段一致。
	got := BuildSubtitleTXT(
		[]string{"主题"},
		[]SubtitleParagraph{
			{Speaker: "1", StartTime: 0, Text: "第一段。"},
			{Speaker: "1", StartTime: 5000, Text: "第二段仍是说话人1。"},
			{Speaker: "2", StartTime: 3_661_000, Text: "跨小时。"},
		},
	)
	want := "关键词\n主题\n\n文字记录\n说话人1 00:00\n第一段。\n\n说话人1 00:05\n第二段仍是说话人1。\n\n说话人2 1:01:01\n跨小时。\n"
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
