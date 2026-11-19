package asr

import (
	"encoding/json"
	"testing"
)

func TestMergeWindowASR_ShiftsBySegmentAlignedOffset(t *testing.T) {
	window := json.RawMessage(`{
		"audio_info":{"duration":12000},
		"result":{"utterances":[
			{"additions":{"speaker":"1"},"start_time":7000,"end_time":8000,"text":"你好","words":[
				{"start_time":7000,"end_time":7500,"text":"你"},
				{"start_time":7500,"end_time":8000,"text":"好"}
			]}
		]}
	}`)
	// 音频从 180s 分片起点开始，cursor 却是 185s：必须按 180000 平移。
	merged, dur, err := MergeWindowASR("{}", window, 180000, 185000)
	if err != nil {
		t.Fatalf("MergeWindowASR: %v", err)
	}
	if dur < 192000 {
		t.Fatalf("duration = %d, want >= 192000", dur)
	}
	var payload liveASRPayload
	if err := json.Unmarshal([]byte(merged), &payload); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	if len(payload.Result.Utterances) != 1 {
		t.Fatalf("utterances = %d, want 1", len(payload.Result.Utterances))
	}
	var u utteranceTimes
	if err := json.Unmarshal(payload.Result.Utterances[0], &u); err != nil {
		t.Fatalf("unmarshal utterance: %v", err)
	}
	if u.StartTime != 187000 || u.EndTime != 188000 {
		t.Fatalf("times = %d-%d, want 187000-188000", u.StartTime, u.EndTime)
	}
	if len(u.Words) != 2 || u.Words[0].StartTime != 187000 || u.Words[1].EndTime != 188000 {
		t.Fatalf("words = %#v", u.Words)
	}
}

func TestMergeWindowASR_SkipsOverlapBeforeCursor(t *testing.T) {
	existing := `{
		"audio_info":{"duration":185000},
		"result":{"utterances":[
			{"additions":{"speaker":"1"},"start_time":184000,"end_time":184800,"text":"上一窗","words":[]}
		]}
	}`
	window := json.RawMessage(`{
		"audio_info":{"duration":12000},
		"result":{"utterances":[
			{"additions":{"speaker":"1"},"start_time":1000,"end_time":2000,"text":"重叠","words":[]},
			{"additions":{"speaker":"1"},"start_time":6000,"end_time":7000,"text":"新句","words":[]}
		]}
	}`)
	merged, _, err := MergeWindowASR(existing, window, 180000, 185000)
	if err != nil {
		t.Fatalf("MergeWindowASR: %v", err)
	}
	var payload liveASRPayload
	if err := json.Unmarshal([]byte(merged), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Result.Utterances) != 2 {
		t.Fatalf("utterances = %d, want 2 (keep existing + new only)", len(payload.Result.Utterances))
	}
	var u utteranceTimes
	if err := json.Unmarshal(payload.Result.Utterances[1], &u); err != nil {
		t.Fatalf("unmarshal new: %v", err)
	}
	if u.Text != "新句" || u.StartTime != 186000 {
		t.Fatalf("new utterance = %#v, want 新句@186000", u)
	}
}

func TestMergeWindowASR_NoSkipWhenCursorZero(t *testing.T) {
	window := json.RawMessage(`{
		"audio_info":{"duration":3000},
		"result":{"utterances":[
			{"additions":{"speaker":"1"},"start_time":0,"end_time":500,"text":"开场","words":[]}
		]}
	}`)
	merged, _, err := MergeWindowASR("{}", window, 0, 0)
	if err != nil {
		t.Fatalf("MergeWindowASR: %v", err)
	}
	var payload liveASRPayload
	if err := json.Unmarshal([]byte(merged), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Result.Utterances) != 1 {
		t.Fatalf("utterances = %d, want 1", len(payload.Result.Utterances))
	}
}
