package asr

import (
	"encoding/json"
	"testing"
)

func TestScaleTimestampsToDuration_StretchesUtterances(t *testing.T) {
	raw := json.RawMessage(`{
		"audio_info":{"duration":100000},
		"result":{"utterances":[
			{"additions":{"speaker":"1"},"start_time":10000,"end_time":20000,"text":"测",
				"words":[{"start_time":10000,"end_time":15000,"text":"测"}]}
		]}
	}`)
	scaled, scale, err := ScaleTimestampsToDuration(raw, 110000)
	if err != nil {
		t.Fatalf("ScaleTimestampsToDuration: %v", err)
	}
	if scale < 1.09 || scale > 1.11 {
		t.Fatalf("scale = %v, want ~1.1", scale)
	}
	if got := ParseDurationMs(scaled); got != 110000 {
		t.Fatalf("duration = %d, want 110000", got)
	}
	var payload liveASRPayload
	if err := json.Unmarshal(scaled, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var u utteranceTimes
	if err := json.Unmarshal(payload.Result.Utterances[0], &u); err != nil {
		t.Fatalf("utterance: %v", err)
	}
	if u.StartTime != 11000 || u.EndTime != 22000 {
		t.Fatalf("times = %d-%d, want 11000-22000", u.StartTime, u.EndTime)
	}
	if len(u.Words) != 1 || u.Words[0].StartTime != 11000 || u.Words[0].EndTime != 16500 {
		t.Fatalf("words = %#v", u.Words)
	}
}

func TestShouldScaleTimestamps(t *testing.T) {
	if !ShouldScaleTimestamps(603324, 609099, 500) {
		t.Fatal("want scale when skew ~5.8s")
	}
	if ShouldScaleTimestamps(609000, 609100, 500) {
		t.Fatal("want skip when skew < threshold")
	}
	if ShouldScaleTimestamps(0, 609099, 500) {
		t.Fatal("want skip when vendor missing")
	}
}
