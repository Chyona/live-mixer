package asr

import (
	"os"
	"testing"
)

const sampleUtteranceJSON = `{
	"result": {
		"utterances": [
			{
				"additions": {"speaker": "1"},
				"end_time": 400,
				"start_time": 40,
				"text": "跳舞吗？",
				"words": [
					{"confidence": 0, "end_time": 160, "start_time": 40, "text": "跳"},
					{"confidence": 0, "end_time": 280, "start_time": 160, "text": "舞"},
					{"confidence": 0, "end_time": 400, "start_time": 360, "text": "吗"}
				]
			}
		]
	}
}`

func TestFormatUtterancesForAPI_Sample(t *testing.T) {
	got := FormatUtterancesForAPI(sampleUtteranceJSON)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Speaker != "1" || got[0].Text != "跳舞吗？" {
		t.Errorf("unexpected utterance: %+v", got[0])
	}
	if len(got[0].Words) != 3 || got[0].Words[0].Text != "跳" {
		t.Errorf("unexpected words: %+v", got[0].Words)
	}
}

func TestFormatUtterancesForAPI_Empty(t *testing.T) {
	for _, input := range []string{"", "{}", `{"result":{}}`} {
		got := FormatUtterancesForAPI(input)
		if len(got) != 0 {
			t.Errorf("input %q: len = %d, want 0", input, len(got))
		}
	}
}

func TestFormatUtterancesForAPI_LiveASRFile(t *testing.T) {
	raw, err := os.ReadFile("../../live_asr.json")
	if err != nil {
		t.Skipf("live_asr.json not available: %v", err)
	}
	got := FormatUtterancesForAPI(string(raw))
	if len(got) == 0 {
		t.Fatal("expected utterances from live_asr.json")
	}
	if got[0].Speaker == "" || got[0].Text == "" || len(got[0].Words) == 0 {
		t.Errorf("first utterance incomplete: %+v", got[0])
	}
}
