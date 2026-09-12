package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/storage"

	"go.uber.org/zap"
)

const (
	captionDiagFileName       = "caption_diag.json"
	captionDiagMapErrSuspect  = 50   // |map_err_ms| 超过则计入映射异常
	captionDiagDeltaSuspectMS = 300  // |delta_ms| 超过则计入裁切时长嫌疑
	captionDiagMaxSkewMS      = 2500 // 与 resolveClipDraftDurationMS 一致
	captionDiagOnsetSuspectMS = 120  // |onset_shift_ms| 超过则 suspect_content
	captionDiagOnsetWindowMS  = 400  // 片头抽样音频长度
)

// CaptionDiagReport 字幕/音画对齐诊断报告（方案 A）。
type CaptionDiagReport struct {
	JobID           string             `json:"job_id"`
	SourceURL       string             `json:"source_url,omitempty"`
	SourceMode      string             `json:"source_mode,omitempty"`
	CutMode         string             `json:"cut_mode"`
	FastKeyframe    bool               `json:"fast_keyframe"`
	TotalWantMS     int64              `json:"total_want_ms"`
	CaptionsEnabled bool               `json:"captions_enabled"`
	Clips           []CaptionDiagClip  `json:"clips"`
	Captions        []CaptionDiagItem  `json:"captions"`
	Summary         CaptionDiagSummary `json:"summary"`
	Hint            string             `json:"hint"`
}

// CaptionDiagClip 单段切片时长对比。
type CaptionDiagClip struct {
	Index          int    `json:"index"`
	Path           string `json:"path,omitempty"`
	SourceStartMS  int64  `json:"source_start_ms"`
	SourceEndMS    int64  `json:"source_end_ms"`
	WantMS         int64  `json:"want_ms"`
	ActualMS       int64  `json:"actual_ms"`
	DeltaMS        int64  `json:"delta_ms"`
	DraftStartUS   int64  `json:"draft_start_us"`
	DraftEndUS     int64  `json:"draft_end_us"`
	Scale          float64 `json:"scale"`
	UsedProbe      bool   `json:"used_probe"`
	SuspectCut     bool   `json:"suspect_cut"`
	OnsetShiftMS   *int64 `json:"onset_shift_ms,omitempty"`
	SuspectContent bool   `json:"suspect_content"`
}

// CaptionDiagItem 单条字幕映射误差。
type CaptionDiagItem struct {
	ClipIndex    int    `json:"clip_index"`
	Text         string `json:"text"`
	ASRStartMS   int64  `json:"asr_start_ms"`
	ASREndMS     int64  `json:"asr_end_ms"`
	DraftStartUS int64  `json:"draft_start_us"`
	DraftEndUS   int64  `json:"draft_end_us"`
	RelASRMS     int64  `json:"rel_asr_ms"`
	RelDraftMS   int64  `json:"rel_draft_ms"`
	MapErrMS     int64  `json:"map_err_ms"`
	SuspectMap   bool   `json:"suspect_map"`
}

// CaptionDiagSummary 汇总统计，便于快速分流 L1/L2/L3/L4。
type CaptionDiagSummary struct {
	ClipCount              int     `json:"clip_count"`
	CaptionCount           int     `json:"caption_count"`
	SuspectCutClips        int     `json:"suspect_cut_clips"`
	SuspectContentClips    int     `json:"suspect_content_clips"`
	SuspectMapCaptions     int     `json:"suspect_map_captions"`
	SuspectMapRatio        float64 `json:"suspect_map_ratio"`
	DeltaMSMedian          int64   `json:"delta_ms_median"`
	DeltaMSP90             int64   `json:"delta_ms_p90"`
	MapErrMSMedianAbs      int64   `json:"map_err_ms_median_abs"`
	MapErrMSP90Abs         int64   `json:"map_err_ms_p90_abs"`
	ASRUtteranceCount      int     `json:"asr_utterance_count"`
	ASRWindowGapCount      int     `json:"asr_window_gap_count"`
	ASRMaxWindowGapMS      int64   `json:"asr_max_window_gap_ms"`
	ASRWindowGapBoundaries []int64 `json:"asr_window_gap_boundaries_ms,omitempty"`
	LikelyLayer            string  `json:"likely_layer"`
	LikelyLayerDescription string  `json:"likely_layer_description"`
}

// mappedCaption 内部：字幕条目 + 源 ASR 时间，供诊断复用同一映射结果。
type mappedCaption struct {
	Item       capcutmate.CaptionItem
	ClipIndex  int
	ASRStartMS int64
	ASREndMS   int64
	Scale      float64
}

// BuildCaptionsFromASR 将 live_asr JSON 分句映射到草稿字幕时间轴。
func BuildCaptionsFromASR(liveASRJSON string, placements []session.ClipPlacement) []capcutmate.CaptionItem {
	items, _ := mapCaptionsFromASR(liveASRJSON, placements)
	return items
}

func mapCaptionsFromASR(liveASRJSON string, placements []session.ClipPlacement) ([]capcutmate.CaptionItem, []mappedCaption) {
	utterances := asr.FormatUtterancesForAPI(liveASRJSON)
	if len(utterances) == 0 || len(placements) == 0 {
		return nil, nil
	}

	out := make([]capcutmate.CaptionItem, 0)
	mapped := make([]mappedCaption, 0)
	for pi, p := range placements {
		sourceDurMS := p.SourceEndMS - p.SourceStartMS
		draftDurUS := p.DraftEndUS - p.DraftStartUS
		if sourceDurMS <= 0 || draftDurUS <= 0 {
			continue
		}
		scale := float64(draftDurUS) / float64(sourceDurMS*1000)
		for _, u := range utterances {
			if strings.TrimSpace(u.Text) == "" {
				continue
			}
			if u.EndTime <= p.SourceStartMS || u.StartTime >= p.SourceEndMS {
				continue
			}
			for _, seg := range asr.SplitUtteranceForCaptions(u) {
				text := strings.TrimSpace(seg.Text)
				if text == "" {
					continue
				}
				if seg.EndTime <= p.SourceStartMS || seg.StartTime >= p.SourceEndMS {
					continue
				}
				overlapStartMS := seg.StartTime
				if overlapStartMS < p.SourceStartMS {
					overlapStartMS = p.SourceStartMS
				}
				overlapEndMS := seg.EndTime
				if overlapEndMS > p.SourceEndMS {
					overlapEndMS = p.SourceEndMS
				}
				if overlapEndMS <= overlapStartMS {
					continue
				}
				draftStartUS := p.DraftStartUS + int64(float64((overlapStartMS-p.SourceStartMS)*1000)*scale)
				draftEndUS := p.DraftStartUS + int64(float64((overlapEndMS-p.SourceStartMS)*1000)*scale)
				if draftEndUS > p.DraftEndUS {
					draftEndUS = p.DraftEndUS
				}
				if draftStartUS < p.DraftStartUS {
					draftStartUS = p.DraftStartUS
				}
				if draftEndUS <= draftStartUS {
					continue
				}
				item := capcutmate.CaptionItem{
					Start: draftStartUS,
					End:   draftEndUS,
					Text:  text,
				}
				out = append(out, item)
				mapped = append(mapped, mappedCaption{
					Item:       item,
					ClipIndex:  pi,
					ASRStartMS: overlapStartMS,
					ASREndMS:   overlapEndMS,
					Scale:      scale,
				})
			}
		}
	}
	return out, mapped
}

// BuildCaptionDiagReport 根据 Session 切片与 ASR 映射生成对齐诊断报告。
func BuildCaptionDiagReport(ctx context.Context, s *session.Session, prober media.MediaTimelineProber) (*CaptionDiagReport, error) {
	if s == nil {
		return nil, fmt.Errorf("session 不能为空")
	}
	if prober == nil {
		prober = media.NewFFprobeProber("")
	}
	liveASR := ""
	if s.Material != nil {
		liveASR = s.Material.LiveASR
	}
	_, mapped := mapCaptionsFromASR(liveASR, s.ClipPlacements)

	report := &CaptionDiagReport{
		JobID:           s.JobID,
		CutMode:         s.CutMode,
		FastKeyframe:    s.FastKeyframe,
		SourceMode:      s.SourceMode,
		CaptionsEnabled: true,
		Clips:           make([]CaptionDiagClip, 0, len(s.ClipPlacements)),
		Captions:        make([]CaptionDiagItem, 0, len(mapped)),
	}
	if s.Project != nil {
		report.CaptionsEnabled = s.Project.EnableCaptions != model.EnableCaptionsOff
	}
	if s.Material != nil {
		report.SourceURL = s.SourcePath
		if report.SourceURL == "" {
			report.SourceURL = s.Material.ProcessMediaURL()
		}
	}
	if report.SourceMode == "" && media.IsM3U8URL(report.SourceURL) {
		report.SourceMode = "remote_hls"
	}

	deltas := make([]int64, 0, len(s.ClipPlacements))
	for i, p := range s.ClipPlacements {
		wantMS := p.SourceEndMS - p.SourceStartMS
		path := ""
		if i < len(s.ClipPaths) {
			path = s.ClipPaths[i]
		}
		actualMS, usedProbe := probeClipActualMS(ctx, prober, path, wantMS)
		delta := actualMS - wantMS
		draftDurUS := p.DraftEndUS - p.DraftStartUS
		scale := 1.0
		if wantMS > 0 {
			scale = float64(draftDurUS) / float64(wantMS*1000)
		}
		clip := CaptionDiagClip{
			Index:         i,
			Path:          filepath.Base(path),
			SourceStartMS: p.SourceStartMS,
			SourceEndMS:   p.SourceEndMS,
			WantMS:        wantMS,
			ActualMS:      actualMS,
			DeltaMS:       delta,
			DraftStartUS:  p.DraftStartUS,
			DraftEndUS:    p.DraftEndUS,
			Scale:         scale,
			UsedProbe:     usedProbe,
			SuspectCut:    absInt64(delta) >= captionDiagDeltaSuspectMS,
		}
		report.Clips = append(report.Clips, clip)
		report.TotalWantMS += wantMS
		deltas = append(deltas, delta)
		if clip.SuspectCut {
			report.Summary.SuspectCutClips++
		}
	}

	fillOnsetContentChecks(ctx, s, report)

	mapErrsAbs := make([]int64, 0, len(mapped))
	for _, m := range mapped {
		p := s.ClipPlacements[m.ClipIndex]
		relASR := m.ASRStartMS - p.SourceStartMS
		relDraftMS := (m.Item.Start - p.DraftStartUS) / 1000
		expectedRel := int64(float64(relASR) * m.Scale)
		mapErr := relDraftMS - expectedRel
		item := CaptionDiagItem{
			ClipIndex:    m.ClipIndex,
			Text:         truncateRunesDiag(m.Item.Text, 40),
			ASRStartMS:   m.ASRStartMS,
			ASREndMS:     m.ASREndMS,
			DraftStartUS: m.Item.Start,
			DraftEndUS:   m.Item.End,
			RelASRMS:     relASR,
			RelDraftMS:   relDraftMS,
			MapErrMS:     mapErr,
			SuspectMap:   absInt64(mapErr) >= captionDiagMapErrSuspect,
		}
		report.Captions = append(report.Captions, item)
		mapErrsAbs = append(mapErrsAbs, absInt64(mapErr))
		if item.SuspectMap {
			report.Summary.SuspectMapCaptions++
		}
	}

	report.Summary.ClipCount = len(report.Clips)
	report.Summary.CaptionCount = len(report.Captions)
	if report.Summary.CaptionCount > 0 {
		report.Summary.SuspectMapRatio = float64(report.Summary.SuspectMapCaptions) / float64(report.Summary.CaptionCount)
	}
	report.Summary.DeltaMSMedian = percentileSortedInt64(sortedCopy(deltas), 50)
	report.Summary.DeltaMSP90 = percentileSortedInt64(sortedCopyAbs(deltas), 90)
	report.Summary.MapErrMSMedianAbs = percentileSortedInt64(sortedCopy(mapErrsAbs), 50)
	report.Summary.MapErrMSP90Abs = percentileSortedInt64(sortedCopy(mapErrsAbs), 90)
	fillASRWindowGapSummary(liveASR, &report.Summary)
	report.Summary.LikelyLayer, report.Summary.LikelyLayerDescription = classifyCaptionDiag(report.Summary)
	report.Hint = report.Summary.LikelyLayerDescription
	return report, nil
}

func probeClipActualMS(ctx context.Context, prober media.MediaTimelineProber, localPath string, wantMS int64) (actualMS int64, usedProbe bool) {
	if localPath == "" || prober == nil {
		return wantMS, false
	}
	tl, err := prober.ProbeMediaTimeline(ctx, localPath)
	if err != nil {
		return wantMS, false
	}
	actual := tl.DurationMS()
	if actual <= 0 {
		return wantMS, false
	}
	if actual > wantMS+captionDiagMaxSkewMS || wantMS > actual+captionDiagMaxSkewMS {
		return actual, true // still report real actual, but VideosStep would not use it
	}
	return actual, true
}

// fillOnsetContentChecks 抽样校验裁切片头与源 [source_start, +T) 的 onset 偏移。
func fillOnsetContentChecks(ctx context.Context, s *session.Session, report *CaptionDiagReport) {
	if s == nil || report == nil || len(report.Clips) == 0 {
		return
	}
	sampleIdx := selectOnsetSampleIndices(report.Clips)
	if len(sampleIdx) == 0 {
		return
	}
	tmpDir := filepath.Join(s.StagingDir, "onset_diag")
	_ = os.MkdirAll(tmpDir, 0o755)
	defer os.RemoveAll(tmpDir)

	durSec := float64(captionDiagOnsetWindowMS) / 1000.0
	for _, i := range sampleIdx {
		if i < 0 || i >= len(report.Clips) {
			continue
		}
		clipPath := ""
		if i < len(s.ClipPaths) {
			clipPath = s.ClipPaths[i]
		}
		if clipPath == "" {
			continue
		}
		refInput, refStartSec, ok := resolveOnsetRefInput(s, report.Clips[i].SourceStartMS)
		if !ok {
			continue
		}
		refRaw := filepath.Join(tmpDir, fmt.Sprintf("ref_%d.pcm", i))
		clipRaw := filepath.Join(tmpDir, fmt.Sprintf("clip_%d.pcm", i))
		octx, cancel := context.WithTimeout(ctx, 20*time.Second)
		errRef := media.ExtractMonoPCM16Window(octx, "", refInput, refRaw, refStartSec, durSec, media.OnsetSampleRate)
		errClip := media.ExtractMonoPCM16Window(octx, "", clipPath, clipRaw, 0, durSec, media.OnsetSampleRate)
		cancel()
		if errRef != nil || errClip != nil {
			continue
		}
		refPCM, err1 := media.ReadPCM16File(refRaw)
		clipPCM, err2 := media.ReadPCM16File(clipRaw)
		if err1 != nil || err2 != nil {
			continue
		}
		shift, ok := media.OnsetShiftMS(refPCM, clipPCM, media.OnsetSampleRate, 200)
		if !ok {
			continue
		}
		v := shift
		report.Clips[i].OnsetShiftMS = &v
		if absInt64(shift) > captionDiagOnsetSuspectMS {
			report.Clips[i].SuspectContent = true
			report.Summary.SuspectContentClips++
		}
	}
}

// selectOnsetSampleIndices 抽样：首/中/末 + |source_start| 最大的几段。
func selectOnsetSampleIndices(clips []CaptionDiagClip) []int {
	n := len(clips)
	if n == 0 {
		return nil
	}
	set := map[int]struct{}{}
	add := func(i int) {
		if i >= 0 && i < n {
			set[i] = struct{}{}
		}
	}
	add(0)
	add(n / 2)
	add(n - 1)
	type pair struct{ i int; start int64 }
	arr := make([]pair, n)
	for i, c := range clips {
		arr[i] = pair{i: i, start: c.SourceStartMS}
	}
	sort.Slice(arr, func(a, b int) bool { return absInt64(arr[a].start) > absInt64(arr[b].start) })
	for k := 0; k < 3 && k < len(arr); k++ {
		add(arr[k].i)
	}
	out := make([]int, 0, len(set))
	for i := range set {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// resolveOnsetRefInput 解析用于 onset 对照的源媒体与片内起点（秒）。
func resolveOnsetRefInput(s *session.Session, sourceStartMS int64) (input string, startSec float64, ok bool) {
	if s == nil {
		return "", 0, false
	}
	// 拼接主 MP4 成片：对照必须用 master.mp4 + 全局 offset（不是单窗内偏移）。
	if (s.SourceMode == "master_mp4" || s.SourceMode == "media_windows") && s.Material != nil {
		readyMS := s.Material.MasterReadyMS()
		if readyMS > 0 && sourceStartMS >= 0 && sourceStartMS < readyMS {
			for _, dir := range []string{strings.TrimSpace(s.LocalIngestDir), strings.TrimSpace(s.StagingDir)} {
				if dir == "" {
					continue
				}
				p := filepath.Join(dir, liveingest.MasterMP4FileName())
				if st, err := os.Stat(p); err == nil && st.Size() > 0 {
					return p, float64(sourceStartMS) / 1000.0, true
				}
			}
		}
	}
	dir := strings.TrimSpace(s.LocalIngestDir)
	if dir != "" && len(liveingest.GlobLocalSegments(dir)) > 0 {
		nominal := int64(model.LiveSegmentDurationSec) * 1000
		idx := liveingest.LoadMediaTimelineIndex(dir, nominal)
		spans, err := idx.Range(sourceStartMS, sourceStartMS+int64(captionDiagOnsetWindowMS))
		if err == nil && len(spans) > 0 {
			spans = liveingest.AttachFilePaths(dir, spans)
			if st, err := os.Stat(spans[0].FilePath); err == nil && st.Size() > 0 {
				return spans[0].FilePath, float64(spans[0].OffsetMS) / 1000.0, true
			}
		}
	}
	src := strings.TrimSpace(s.SourcePath)
	if src != "" {
		if st, err := os.Stat(src); err == nil && !st.IsDir() && st.Size() > 0 {
			return src, float64(sourceStartMS) / 1000.0, true
		}
		// SourcePath 可能是 ingest 目录
		if st, err := os.Stat(src); err == nil && st.IsDir() {
			final := filepath.Join(src, "final.mp4")
			if st2, err := os.Stat(final); err == nil && st2.Size() > 0 {
				return final, float64(sourceStartMS) / 1000.0, true
			}
		}
	}
	return "", 0, false
}

func classifyCaptionDiag(sum CaptionDiagSummary) (layer, desc string) {
	cutHeavy := sum.ClipCount > 0 && float64(sum.SuspectCutClips)/float64(sum.ClipCount) >= 0.2 &&
		sum.DeltaMSP90 >= captionDiagDeltaSuspectMS
	mapHeavy := sum.SuspectMapRatio >= 0.1 || sum.MapErrMSP90Abs >= captionDiagMapErrSuspect
	windowGapHeavy := sum.ASRWindowGapCount > 0 && sum.ASRMaxWindowGapMS >= 3000
	contentHeavy := sum.ClipCount > 0 && sum.SuspectContentClips > 0 &&
		(sum.SuspectContentClips >= 2 || float64(sum.SuspectContentClips)/float64(sum.ClipCount) >= 0.3)

	switch {
	case sum.CaptionCount == 0 && sum.ClipCount == 0:
		return "none", "无切片与字幕，无法判断对齐"
	case contentHeavy && !mapHeavy:
		return "content_seek_mismatch", "片头 onset 校验显示裁切内容与源起点错位（map 可能仍自洽）；优先修 seg+offset / seek"
	case contentHeavy && mapHeavy:
		return "content_seek_mismatch", "onset 与映射误差同时异常，优先排查裁切内容错位"
	case windowGapHeavy && !cutHeavy && !mapHeavy && !contentHeavy:
		return "L1_window_boundary", "live_asr 在窗口边界附近出现异常空隙/跳跃，优先怀疑窗口 ASR 起点用标称 6s 反推导致跳段"
	case sum.CaptionCount == 0:
		if contentHeavy {
			return "content_seek_mismatch", "无字幕条目；onset 显示裁切内容错位"
		}
		if cutHeavy {
			return "L2_cut", "无字幕条目；多段 actual-want 偏差偏大，优先怀疑裁切起点/时长"
		}
		if windowGapHeavy {
			return "L1_window_boundary", "无草稿字幕条目，但源 ASR 在窗口边界有异常空隙"
		}
		return "none", "无字幕条目，无法判断对齐"
	case cutHeavy && mapHeavy:
		return "L2_and_L1", "裁切偏差与映射误差同时偏高，建议先修裁切再看 ASR"
	case cutHeavy:
		return "L2_cut", "多段 actual-want 偏差偏大，优先怀疑裁切起点/时长（混合 seek 或关键帧）"
	case mapHeavy:
		return "L1_or_L4_asr", "映射公式自洽误差大或个别句异常，优先怀疑 ASR 时间或拆句均分"
	case windowGapHeavy:
		return "L1_window_boundary", "草稿映射自洽，但源 ASR 在窗口边界有异常空隙；成片若选中边界后片段会整体错位"
	default:
		return "ok_or_mild", "统计未显示明显成簇异常；若体感仍不齐：①源视频 10 分钟窗后是否已错（L1）②仅成片错则查裁切内容平移（onset）"
	}
}

// fillASRWindowGapSummary 扫描完整 live_asr：在调度窗口倍数附近的大空隙，提示窗间接缝问题。
func fillASRWindowGapSummary(liveASRJSON string, sum *CaptionDiagSummary) {
	if sum == nil {
		return
	}
	utterances := asr.FormatUtterancesForAPI(liveASRJSON)
	sum.ASRUtteranceCount = len(utterances)
	if len(utterances) < 2 {
		return
	}
	windowMS := int64(model.LiveASRWindowDuration / time.Millisecond)
	if windowMS <= 0 {
		windowMS = 10 * 60 * 1000
	}
	const nearMS = 45 * 1000  // 落在窗边界 ±45s 内
	const gapSuspectMS = 3500 // 空隙超过该值视为可疑（正常句间停顿通常更短）
	var prevEnd int64 = -1
	for _, u := range utterances {
		if prevEnd >= 0 {
			gap := u.StartTime - prevEnd
			if gap >= gapSuspectMS {
				mid := prevEnd + gap/2
				boundary := ((mid + windowMS/2) / windowMS) * windowMS
				if boundary > 0 && absInt64(mid-boundary) <= nearMS {
					sum.ASRWindowGapCount++
					if gap > sum.ASRMaxWindowGapMS {
						sum.ASRMaxWindowGapMS = gap
					}
					sum.ASRWindowGapBoundaries = appendUniqueInt64(sum.ASRWindowGapBoundaries, boundary)
				}
			}
		}
		if u.EndTime > prevEnd {
			prevEnd = u.EndTime
		}
	}
}

func appendUniqueInt64(vals []int64, v int64) []int64 {
	for _, x := range vals {
		if x == v {
			return vals
		}
	}
	return append(vals, v)
}

// WriteCaptionDiagReport 写入 staging/caption_diag.json。
func WriteCaptionDiagReport(stagingDir string, report *CaptionDiagReport) (string, error) {
	if report == nil {
		return "", fmt.Errorf("report 为空")
	}
	stagingDir = strings.TrimSpace(stagingDir)
	if stagingDir == "" {
		return "", fmt.Errorf("stagingDir 为空")
	}
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(stagingDir, captionDiagFileName)
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// BuildCaptionDiagObjectKey 对象键：temp/draft/{jobID}/caption_diag.json。
func BuildCaptionDiagObjectKey(jobID string) string {
	id := strings.TrimSpace(jobID)
	if id == "" {
		id = "unknown"
	}
	return path.Join(storage.SubDirTemp, "draft", id, captionDiagFileName)
}

// WriteAndUploadCaptionDiag 生成诊断报告、落盘并上传；失败返回 error（调用方可降级为仅记日志）。
func WriteAndUploadCaptionDiag(ctx context.Context, s *session.Session, uploader ObjectUploader, prober media.MediaTimelineProber, logger *zap.Logger) (localPath, url string, err error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	report, err := BuildCaptionDiagReport(ctx, s, prober)
	if err != nil {
		return "", "", err
	}
	localPath, err = WriteCaptionDiagReport(s.StagingDir, report)
	if err != nil {
		return "", "", err
	}
	logger.Info("已写入字幕对齐诊断报告",
		zap.String("job_id", s.JobID),
		zap.String("path", localPath),
		zap.String("likely_layer", report.Summary.LikelyLayer),
		zap.Int("suspect_cut_clips", report.Summary.SuspectCutClips),
		zap.Int("suspect_map_captions", report.Summary.SuspectMapCaptions),
		zap.Float64("suspect_map_ratio", report.Summary.SuspectMapRatio),
	)
	if uploader == nil {
		return localPath, "", nil
	}
	url, err = uploader.UploadFile(ctx, localPath, BuildCaptionDiagObjectKey(s.JobID))
	if err != nil {
		return localPath, "", fmt.Errorf("上传 caption_diag 失败: %w", err)
	}
	return localPath, url, nil
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func sortedCopy(vals []int64) []int64 {
	out := append([]int64(nil), vals...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedCopyAbs(vals []int64) []int64 {
	out := make([]int64, len(vals))
	for i, v := range vals {
		out[i] = absInt64(v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func percentileSortedInt64(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := int(math.Ceil(float64(p)/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func truncateRunesDiag(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
