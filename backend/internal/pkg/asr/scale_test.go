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
	if !ShouldScaleTimestamps(600000, 620000, 5000) {
		t.Fatal("want scale when skew >= 5s")
	}
	if ShouldScaleTimestamps(606132, 608719, 5000) {
		t.Fatal("sub-5s metadata skew must not scale")
	}
	if ShouldScaleTimestamps(0, 609099, 5000) {
		t.Fatal("want skip when vendor missing")
	}
}

func TestShouldScaleUtteranceTimestamps_SkipsWhenUtterancesFitMedia(t *testing.T) {
	if ShouldScaleUtteranceTimestamps(606132, 608719, 606000, 5000) {
		t.Fatal("utterances within media must not scale")
	}
	if !ShouldScaleUtteranceTimestamps(650000, 600000, 649000, 5000) {
		t.Fatal("want scale when utterance domain exceeds media")
	}
}

func TestMaxUtteranceEndMS(t *testing.T) {
	raw := json.RawMessage(`{
		"audio_info":{"duration":100000},
		"result":{"utterances":[
			{"start_time":10000,"end_time":20000,"text":"a","words":[{"start_time":10000,"end_time":25000,"text":"a"}]}
		]}
	}`)
	if got := MaxUtteranceEndMS(raw); got != 25000 {
		t.Fatalf("MaxUtteranceEndMS = %d, want 25000", got)
	}
}

func TestSetAudioInfoDuration(t *testing.T) {
	raw := json.RawMessage(`{"audio_info":{"duration":100},"result":{"utterances":[{"start_time":10,"end_time":20,"text":"a"}]}}`)
	out, err := SetAudioInfoDuration(raw, 608719)
	if err != nil {
		t.Fatal(err)
	}
	if ParseDurationMs(out) != 608719 {
		t.Fatalf("duration = %d", ParseDurationMs(out))
	}
	if MaxUtteranceEndMS(out) != 20 {
		t.Fatal("utterances must be unchanged")
	}
}
