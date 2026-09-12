package model

// AlignDiagSnapshot 素材详情中的对齐诊断摘要（供前端 isdebug 对照预览/ASR 源）。
type AlignDiagSnapshot struct {
	PlayURL         string `json:"play_url"`
	LiveURL         string `json:"live_url"`
	SamePlayAndLive bool   `json:"same_play_and_live"`
	MasterReadyMS   int64  `json:"master_ready_ms"`
	DurationMS      int64  `json:"duration_ms"`
	WindowCount     int    `json:"window_count"`
	ASRCursorMS     int64  `json:"asr_cursor_ms"`
	ASRProgress     int16  `json:"asr_progress"`
	LiveStatus      string `json:"live_status"`
	Hint            string `json:"hint"`
}
