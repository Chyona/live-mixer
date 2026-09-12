package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/media"

	"go.uber.org/zap"
)

const alignDiagFileName = "align_diag.jsonl"

// AlignDiagEvent 跟播音画/字幕对齐诊断事件（追加写入 staging/align_diag.jsonl，并打结构化日志）。
type AlignDiagEvent struct {
	TS         string `json:"ts"`
	MaterialID uint   `json:"material_id"`
	Event      string `json:"event"`

	WindowIndex    int    `json:"window_index,omitempty"`
	WindowCount    int    `json:"window_count,omitempty"`
	LocalPath      string `json:"local_path,omitempty"`
	URL            string `json:"url,omitempty"`
	PlayURL        string `json:"play_url,omitempty"`
	LiveURL        string `json:"live_url,omitempty"`
	ASRSource      string `json:"asr_source,omitempty"` // local_master | downloaded_cdn
	SamePlayLive   *bool  `json:"same_play_and_live,omitempty"`
	MasterReplaced bool   `json:"master_replaced,omitempty"`
	UploadOK       *bool  `json:"upload_ok,omitempty"`
	Error          string `json:"error,omitempty"`

	VideoStartMS int64 `json:"video_start_ms,omitempty"`
	VideoDurMS   int64 `json:"video_dur_ms,omitempty"`
	AudioStartMS int64 `json:"audio_start_ms,omitempty"`
	AudioDurMS   int64 `json:"audio_dur_ms,omitempty"`
	FormatDurMS  int64 `json:"format_dur_ms,omitempty"`
	AVSkewMS     int64 `json:"av_skew_ms,omitempty"` // audio_dur - video_dur
	Width        int   `json:"width,omitempty"`
	Height       int   `json:"height,omitempty"`

	ASRCursorMS   int64   `json:"asr_cursor_ms,omitempty"`
	ReadyMS       int64   `json:"ready_ms,omitempty"`
	ChunkMS       int64   `json:"chunk_ms,omitempty"`
	LeadPadMS     int64   `json:"lead_pad_ms,omitempty"`
	TrimStartSec  float64 `json:"trim_start_sec,omitempty"`
	VendorDurMS   int64   `json:"vendor_dur_ms,omitempty"`
	MaxUttEndMS   int64   `json:"max_utt_end_ms,omitempty"`
	MinUttStartMS int64   `json:"min_utt_start_ms,omitempty"`
	ScaleFactor   float64 `json:"scale_factor,omitempty"`
	Hint          string  `json:"hint,omitempty"`
}

func (w *liveIngestWorker) emitAlignDiag(material *model.LiveMaterial, ev AlignDiagEvent) {
	if material == nil {
		return
	}
	ev.MaterialID = material.ID
	if ev.TS == "" {
		ev.TS = time.Now().Format(time.RFC3339Nano)
	}
	if ev.PlayURL == "" {
		ev.PlayURL = material.PlayURL()
	}
	if ev.LiveURL == "" {
		ev.LiveURL = strings.TrimSpace(material.LiveURL)
	}
	if ev.SamePlayLive == nil && ev.PlayURL != "" && ev.LiveURL != "" {
		same := stripURLQuery(ev.PlayURL) == stripURLQuery(ev.LiveURL)
		ev.SamePlayLive = &same
	}
	if ev.Hint == "" {
		ev.Hint = alignDiagHint(ev)
	}

	fields := []zap.Field{
		zap.Uint("material_id", ev.MaterialID),
		zap.String("event", ev.Event),
		zap.String("hint", ev.Hint),
	}
	if ev.LocalPath != "" {
		fields = append(fields, zap.String("local_path", ev.LocalPath))
	}
	if ev.URL != "" {
		fields = append(fields, zap.String("url", ev.URL))
	}
	if ev.PlayURL != "" {
		fields = append(fields, zap.String("play_url", ev.PlayURL))
	}
	if ev.LiveURL != "" {
		fields = append(fields, zap.String("live_url", ev.LiveURL))
	}
	if ev.ASRSource != "" {
		fields = append(fields, zap.String("asr_source", ev.ASRSource))
	}
	if ev.SamePlayLive != nil {
		fields = append(fields, zap.Bool("same_play_and_live", *ev.SamePlayLive))
	}
	if ev.WindowIndex != 0 || strings.Contains(ev.Event, "window") {
		fields = append(fields, zap.Int("window_index", ev.WindowIndex))
	}
	if ev.WindowCount > 0 {
		fields = append(fields, zap.Int("window_count", ev.WindowCount))
	}
	if ev.VideoDurMS > 0 || ev.AudioDurMS > 0 || ev.AVSkewMS != 0 {
		fields = append(fields,
			zap.Int64("video_start_ms", ev.VideoStartMS),
			zap.Int64("video_dur_ms", ev.VideoDurMS),
			zap.Int64("audio_start_ms", ev.AudioStartMS),
			zap.Int64("audio_dur_ms", ev.AudioDurMS),
			zap.Int64("format_dur_ms", ev.FormatDurMS),
			zap.Int64("av_skew_ms", ev.AVSkewMS),
		)
	}
	if ev.Width > 0 {
		fields = append(fields, zap.Int("width", ev.Width), zap.Int("height", ev.Height))
	}
	if ev.ReadyMS > 0 || ev.ChunkMS > 0 || ev.ASRCursorMS > 0 || ev.VendorDurMS > 0 {
		fields = append(fields,
			zap.Int64("asr_cursor_ms", ev.ASRCursorMS),
			zap.Int64("ready_ms", ev.ReadyMS),
			zap.Int64("chunk_ms", ev.ChunkMS),
			zap.Int64("lead_pad_ms", ev.LeadPadMS),
			zap.Float64("trim_start_sec", ev.TrimStartSec),
			zap.Int64("vendor_dur_ms", ev.VendorDurMS),
			zap.Int64("min_utt_start_ms", ev.MinUttStartMS),
			zap.Int64("max_utt_end_ms", ev.MaxUttEndMS),
			zap.Float64("scale_factor", ev.ScaleFactor),
		)
	}
	if ev.UploadOK != nil {
		fields = append(fields, zap.Bool("upload_ok", *ev.UploadOK))
	}
	if ev.MasterReplaced {
		fields = append(fields, zap.Bool("master_replaced", true))
	}
	if ev.Error != "" {
		fields = append(fields, zap.String("error", ev.Error))
		w.logger.Warn("音画字幕对齐诊断", fields...)
	} else {
		w.logger.Info("音画字幕对齐诊断", fields...)
	}

	w.appendAlignDiagFile(material, ev)
}

func (w *liveIngestWorker) appendAlignDiagFile(material *model.LiveMaterial, ev AlignDiagEvent) {
	dir := w.segmentDir(material)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, alignDiagFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
}

func (w *liveIngestWorker) probeAlignTimeline(ctx context.Context, path string) (media.MediaTimeline, bool) {
	if w.prober == nil || strings.TrimSpace(path) == "" {
		return media.MediaTimeline{}, false
	}
	tl, err := w.prober.ProbeMediaTimeline(ctx, path)
	if err != nil {
		return media.MediaTimeline{}, false
	}
	return tl, true
}

func fillAlignTimeline(ev *AlignDiagEvent, tl media.MediaTimeline) {
	ev.VideoStartMS = int64(tl.VideoStartSec * 1000)
	ev.VideoDurMS = int64(tl.VideoDurationSec * 1000)
	ev.AudioStartMS = int64(tl.AudioStartSec * 1000)
	ev.AudioDurMS = int64(tl.AudioDurationSec * 1000)
	ev.FormatDurMS = int64(tl.FormatDurationSec * 1000)
	ev.AVSkewMS = ev.AudioDurMS - ev.VideoDurMS
	ev.Width = tl.Width
	ev.Height = tl.Height
}

func alignDiagHint(ev AlignDiagEvent) string {
	switch {
	case ev.Error != "" && strings.Contains(ev.Event, "master"):
		return "master_build_failed_preview_asr_may_stuck_on_previous_master"
	case ev.AVSkewMS > 500 || ev.AVSkewMS < -500:
		return "av_duration_skew_in_file_likely_desync_source"
	case ev.SamePlayLive != nil && !*ev.SamePlayLive && ev.PlayURL != "" && ev.LiveURL != "":
		return "play_url_and_live_url_differ_check_preview_vs_asr_source"
	case ev.ASRSource == "downloaded_cdn":
		return "asr_used_cdn_download_local_master_missing"
	case ev.ASRSource == "local_master":
		return "asr_used_local_master_compare_with_play_url_cdn"
	case ev.LeadPadMS > 0 || ev.TrimStartSec > 0:
		return "asr_chunk0_applied_av_align_pad_or_trim"
	case ev.ScaleFactor > 0 && (ev.ScaleFactor < 0.98 || ev.ScaleFactor > 1.02):
		return "asr_timestamps_scaled_to_chunk_duration"
	case ev.ChunkMS > 0 && ev.MaxUttEndMS > 0 && ev.MaxUttEndMS > ev.ChunkMS+2000:
		return "vendor_utt_end_exceeds_chunk_check_scale"
	default:
		return "ok_check_av_skew_and_utt_times_if_ui_desync"
	}
}

func stripURLQuery(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

func resolveMasterASRSource(existedBefore bool) string {
	if existedBefore {
		return "local_master"
	}
	return "downloaded_cdn"
}

func utteranceBoundsMS(raw []byte) (minStart, maxEnd int64) {
	if len(raw) == 0 {
		return 0, 0
	}
	return asr.MinUtteranceStartMS(raw), asr.MaxUtteranceEndMS(raw)
}

// BuildAlignDiagSnapshot 供详情 API 返回，便于前端 isdebug 对照。
func BuildAlignDiagSnapshot(material *model.LiveMaterial) *model.AlignDiagSnapshot {
	if material == nil {
		return nil
	}
	play := material.PlayURL()
	live := strings.TrimSpace(material.LiveURL)
	windows := material.ParsedMediaWindows()
	same := false
	if play != "" && live != "" {
		same = stripURLQuery(play) == stripURLQuery(live)
	}
	snap := &model.AlignDiagSnapshot{
		PlayURL:         play,
		LiveURL:         live,
		SamePlayAndLive: same,
		MasterReadyMS:   material.MasterReadyMS(),
		DurationMS:      material.Duration,
		WindowCount:     windows.ReadyCount(),
		ASRCursorMS:     material.ASRCursorMS,
		ASRProgress:     material.ASRProgress,
		LiveStatus:      material.LiveStatus,
	}
	if !same && play != "" && live != "" {
		snap.Hint = "play_url_and_live_url_differ"
	} else if snap.WindowCount == 0 {
		snap.Hint = "no_ready_window_yet"
	} else {
		snap.Hint = "compare_play_url_with_align_diag_jsonl_asr_local_path"
	}
	return snap
}
