package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"live-mixer/internal/config"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/storage"
	"live-mixer/internal/service"

	"go.uber.org/zap"
)

type liveWindowArgs struct {
	ConfigPath string
	URL        string
	WorkDir    string
	OutPath    string
	MaxWindows int
	MaxSegs    int
	SkipASR    bool
	RepoRoot   string
}

type liveWindowReport struct {
	GeneratedAt      string             `json:"generated_at"`
	SourceURL        string             `json:"source_url"`
	WorkDir          string             `json:"work_dir"`
	SkipASR          bool               `json:"skip_asr"`
	PlaylistSegCount int                `json:"playlist_seg_count"`
	DownloadedSegs   int                `json:"downloaded_segs"`
	TotalMediaMS     int64              `json:"total_media_ms"`
	TotalMediaClock  string             `json:"total_media_clock"`
	WindowCapSegs    int                `json:"window_cap_segs"`
	ASRWindow        string             `json:"asr_window"`
	FinalCursorMS    int64              `json:"final_cursor_ms"`
	UtteranceCount   int                `json:"utterance_count"`
	Windows          []liveWindowRound  `json:"windows"`
	LiveASR          json.RawMessage    `json:"live_asr"`
	Notes            []string           `json:"notes,omitempty"`
	Errors           []string           `json:"errors,omitempty"`
}

type liveWindowRound struct {
	Index            int    `json:"index"`
	CursorBeforeMS   int64  `json:"cursor_before_ms"`
	StartSeg         int64  `json:"start_seg"`
	OffsetMS         int64  `json:"offset_ms"`
	NominalStartSeg  int64  `json:"nominal_start_seg"`
	NominalOffsetMS  int64  `json:"nominal_offset_ms"`
	SegCount         int    `json:"seg_count"`
	Truncated        bool   `json:"truncated"`
	WindowMediaMS    int64  `json:"window_media_ms"`
	CursorAfterMS    int64  `json:"cursor_after_ms"`
	ASRVendorDurMS   int64  `json:"asr_vendor_duration_ms,omitempty"`
	UtterancesAdded  int    `json:"utterances_added"`
	ElapsedMS        int64  `json:"elapsed_ms"`
	SkippedASR       bool   `json:"skipped_asr"`
	Error            string `json:"error,omitempty"`
}

type playlistSeg struct {
	Index       int
	DurationSec float64
	URL         string
}

func runLiveWindowMode(a liveWindowArgs) {
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}
	logger, err := zap.NewDevelopment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	if err := os.MkdirAll(a.WorkDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建工作目录失败: %v\n", err)
		os.Exit(1)
	}
	segDir := filepath.Join(a.WorkDir, "segs")
	_ = os.MkdirAll(segDir, 0o755)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()

	fmt.Fprintf(os.Stderr, "模式: live-window\nURL: %s\n工作目录: %s\n", a.URL, a.WorkDir)
	segs, err := fetchPlaylistSegs(ctx, a.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 m3u8 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "播放列表分片数: %d\n", len(segs))
	playlistTotal := len(segs)

	capSegs := maxWindowASRSegmentsLocal()
	need := a.MaxSegs
	if need <= 0 {
		win := a.MaxWindows
		if win <= 0 {
			win = 2
		}
		need = win * capSegs
	}
	if need > len(segs) {
		need = len(segs)
	}
	segs = segs[:need]
	fmt.Fprintf(os.Stderr, "将下载/处理前 %d 片（窗上限 %d 片/窗）\n", len(segs), capSegs)

	localPaths := make([]string, 0, len(segs))
	durations := make([]int64, 0, len(segs))
	prober := media.NewFFprobeProber("")
	nominalMS := int64(model.LiveSegmentDurationSec) * 1000
	httpClient := &http.Client{Timeout: 2 * time.Minute}

	for i, s := range segs {
		name := fmt.Sprintf("seg_%05d.ts", i)
		local := filepath.Join(segDir, name)
		if st, err := os.Stat(local); err != nil || st.Size() == 0 {
			fmt.Fprintf(os.Stderr, "下载 [%d/%d] %s\n", i+1, len(segs), name)
			if err := downloadFile(ctx, httpClient, s.URL, local); err != nil {
				fmt.Fprintf(os.Stderr, "下载失败 %s: %v\n", s.URL, err)
				os.Exit(1)
			}
		}
		localPaths = append(localPaths, local)
		ms := probeLocalDurationMS(ctx, prober, local, nominalMS)
		if s.DurationSec > 0 {
			// 列表 EXTINF 也可参考；仍以本地探测为准
			_ = s.DurationSec
		}
		durations = append(durations, ms)
	}

	var totalMS int64
	for _, d := range durations {
		totalMS += d
	}

	report := liveWindowReport{
		GeneratedAt:      time.Now().Format(time.RFC3339),
		SourceURL:        a.URL,
		WorkDir:          a.WorkDir,
		SkipASR:          a.SkipASR,
		PlaylistSegCount: playlistTotal,
		DownloadedSegs:   len(localPaths),
		TotalMediaMS:     totalMS,
		TotalMediaClock:  model.FormatClockMS(totalMS),
		WindowCapSegs:    capSegs,
		ASRWindow:        model.LiveASRWindowDuration.String(),
		Notes: []string{
			"模拟跟播窗口 ASR：ResolveWindowStartByDurations + 单窗分片上限 + MergeWindowASR",
			"游标按本窗真实媒体时长推进（拼接文件探测）",
		},
	}

	var (
		store    *storage.Client
		asrSvc   service.ASRService
		ffmpeg   = media.NewFFmpegConverter("")
		liveASR  = "{}"
		cursorMS int64
	)
	if !a.SkipASR {
		if strings.TrimSpace(cfg.ASR.APIKey) == "" {
			fmt.Fprintln(os.Stderr, "ASR API Key 未配置（APP_ASR_API_KEY / asr.api_key）")
			os.Exit(1)
		}
		store, err = storage.NewClientFromAppConfig(cfg.Storage)
		if err != nil {
			fmt.Fprintf(os.Stderr, "对象存储未配置，ASR 需要上传音频: %v\n", err)
			os.Exit(1)
		}
		asrSvc = service.NewASRServiceFromConfig(cfg.ASR.ASRClientConfig())
	}

	maxWin := a.MaxWindows
	if maxWin < 0 {
		maxWin = 0
	}
	round := 0
	for {
		if maxWin > 0 && round >= maxWin {
			break
		}
		if cursorMS >= totalMS {
			break
		}
		round++
		started := time.Now()
		nominalStart := liveingest.WindowStartIndex(cursorMS, model.LiveSegmentDurationSec)
		startSeg, offsetMS := liveingest.ResolveWindowStartByDurations(durations, cursorMS, 1)
		rec := liveWindowRound{
			Index:           round,
			CursorBeforeMS:  cursorMS,
			StartSeg:        startSeg,
			OffsetMS:        offsetMS,
			NominalStartSeg: nominalStart,
			NominalOffsetMS: liveingest.WindowOffsetMS(cursorMS, model.LiveSegmentDurationSec),
		}
		if int(startSeg) >= len(localPaths) {
			rec.Error = "start_seg 超出本地分片"
			report.Windows = append(report.Windows, rec)
			report.Errors = append(report.Errors, rec.Error)
			break
		}
		files := localPaths[int(startSeg):]
		truncated := false
		if len(files) > capSegs {
			files = files[:capSegs]
			truncated = true
		}
		rec.SegCount = len(files)
		rec.Truncated = truncated

		fmt.Fprintf(os.Stderr, "窗口 #%d cursor=%dms start_seg=%d (nominal=%d) files=%d truncated=%v\n",
			round, cursorMS, startSeg, nominalStart, len(files), truncated)

		concatPath := filepath.Join(a.WorkDir, fmt.Sprintf("win_%03d.ts", round))
		mp3Path := filepath.Join(a.WorkDir, fmt.Sprintf("win_%03d.mp3", round))
		if err := ffmpeg.ConcatMediaFiles(ctx, files, concatPath); err != nil {
			rec.Error = fmt.Sprintf("拼接失败: %v", err)
			rec.ElapsedMS = time.Since(started).Milliseconds()
			report.Windows = append(report.Windows, rec)
			report.Errors = append(report.Errors, rec.Error)
			break
		}
		windowMediaMS := probeLocalDurationMS(ctx, prober, concatPath, int64(len(files))*nominalMS)
		rec.WindowMediaMS = windowMediaMS
		newCursor := offsetMS + windowMediaMS
		if newCursor < cursorMS {
			newCursor = cursorMS
		}
		rec.CursorAfterMS = newCursor

		beforeUtt := len(asr.FormatUtterancesForAPI(liveASR))
		if a.SkipASR {
			rec.SkippedASR = true
			cursorMS = newCursor
			rec.ElapsedMS = time.Since(started).Milliseconds()
			report.Windows = append(report.Windows, rec)
			_ = os.Remove(concatPath)
			continue
		}

		if err := ffmpeg.ConvertToASRMP3(ctx, concatPath, mp3Path); err != nil {
			rec.Error = fmt.Sprintf("转 MP3 失败: %v", err)
			rec.ElapsedMS = time.Since(started).Milliseconds()
			report.Windows = append(report.Windows, rec)
			report.Errors = append(report.Errors, rec.Error)
			break
		}
		objectKey := path.Join(storage.SubDirTemp, "cmd-test", fmt.Sprintf("live-window-%d-%d.mp3", time.Now().Unix(), round))
		audioURL, err := store.UploadFile(ctx, mp3Path, objectKey)
		if err != nil {
			rec.Error = fmt.Sprintf("上传失败: %v", err)
			rec.ElapsedMS = time.Since(started).Milliseconds()
			report.Windows = append(report.Windows, rec)
			report.Errors = append(report.Errors, rec.Error)
			break
		}
		fmt.Fprintf(os.Stderr, "  Transcribe... %s\n", audioURL)
		raw, err := asrSvc.Transcribe(ctx, audioURL)
		if err != nil {
			rec.Error = fmt.Sprintf("ASR 失败: %v", err)
			rec.ElapsedMS = time.Since(started).Milliseconds()
			report.Windows = append(report.Windows, rec)
			report.Errors = append(report.Errors, rec.Error)
			logger.Warn("窗口 ASR 失败", zap.Error(err))
			// 仍推进游标，避免死循环；与线上「识别失败跳过本窗」不同，工具侧继续以便看后续窗
			cursorMS = newCursor
			break
		}
		rec.ASRVendorDurMS = asr.ParseDurationMs(raw)
		merged, _, err := asr.MergeWindowASR(liveASR, raw, offsetMS, cursorMS)
		if err != nil {
			rec.Error = fmt.Sprintf("合并失败: %v", err)
			rec.ElapsedMS = time.Since(started).Milliseconds()
			report.Windows = append(report.Windows, rec)
			report.Errors = append(report.Errors, rec.Error)
			break
		}
		liveASR = merged
		afterUtt := len(asr.FormatUtterancesForAPI(liveASR))
		rec.UtterancesAdded = afterUtt - beforeUtt
		cursorMS = newCursor
		rec.ElapsedMS = time.Since(started).Milliseconds()
		report.Windows = append(report.Windows, rec)
		fmt.Fprintf(os.Stderr, "  完成 cursor=%dms (+%d 句) elapsed=%dms\n",
			cursorMS, rec.UtterancesAdded, rec.ElapsedMS)

		_ = os.Remove(concatPath)
		_ = os.Remove(mp3Path)

		if newCursor <= rec.CursorBeforeMS {
			report.Notes = append(report.Notes, fmt.Sprintf("窗口 #%d 游标未推进，停止", round))
			break
		}
	}

	report.FinalCursorMS = cursorMS
	report.UtteranceCount = len(asr.FormatUtterancesForAPI(liveASR))
	if liveASR != "" && liveASR != "{}" {
		report.LiveASR = json.RawMessage(liveASR)
	} else {
		report.LiveASR = json.RawMessage(`{}`)
	}

	// 窗边界空隙提示（与 caption_diag 同类检查）
	if gaps := findWindowBoundaryGaps(liveASR, int64(model.LiveASRWindowDuration/time.Millisecond)); len(gaps) > 0 {
		report.Notes = append(report.Notes, fmt.Sprintf("窗口边界可疑空隙 %d 处: %v", len(gaps), gaps))
	}

	mustWriteJSON(a.OutPath, report)
	fmt.Fprintf(os.Stderr, "已写入报告: %s\n", a.OutPath)
	fmt.Fprintf(os.Stderr, "汇总: segs=%d total=%s cursor=%dms utterances=%d windows=%d errors=%d\n",
		report.DownloadedSegs, report.TotalMediaClock, report.FinalCursorMS, report.UtteranceCount, len(report.Windows), len(report.Errors))
}

func maxWindowASRSegmentsLocal() int {
	segSec := model.LiveSegmentDurationSec
	if segSec <= 0 {
		segSec = 6
	}
	windowSec := int(model.LiveASRWindowDuration / time.Second)
	if windowSec <= 0 {
		windowSec = 10 * 60
	}
	n := windowSec / segSec
	if n < 1 {
		n = 1
	}
	return n + 1
}

func probeLocalDurationMS(ctx context.Context, prober media.MediaTimelineProber, path string, fallback int64) int64 {
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tl, err := prober.ProbeMediaTimeline(pctx, path)
	if err != nil {
		return fallback
	}
	if d := tl.DurationMS(); d > 0 {
		return d
	}
	return fallback
}

func fetchPlaylistSegs(ctx context.Context, playlistURL string) ([]playlistSeg, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playlistURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(playlistURL)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(body), "\n")
	out := make([]playlistSeg, 0, 256)
	var pendingDur float64
	idx := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			raw := strings.TrimPrefix(line, "#EXTINF:")
			raw = strings.TrimSuffix(raw, ",")
			if i := strings.IndexByte(raw, ','); i >= 0 {
				raw = raw[:i]
			}
			pendingDur, _ = strconv.ParseFloat(raw, 64)
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		u, err := base.Parse(line)
		if err != nil {
			return nil, err
		}
		out = append(out, playlistSeg{Index: idx, DurationSec: pendingDur, URL: u.String()})
		idx++
		pendingDur = 0
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("播放列表无分片")
	}
	return out, nil
}

func downloadFile(ctx context.Context, client *http.Client, rawURL, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dest)
}

func findWindowBoundaryGaps(liveASR string, windowMS int64) []int64 {
	if windowMS <= 0 {
		windowMS = 10 * 60 * 1000
	}
	utts := asr.FormatUtterancesForAPI(liveASR)
	if len(utts) < 2 {
		return nil
	}
	const nearMS = 45 * 1000
	const gapSuspectMS = 3500
	var out []int64
	prevEnd := int64(-1)
	seen := map[int64]struct{}{}
	for _, u := range utts {
		if prevEnd >= 0 {
			gap := u.StartTime - prevEnd
			if gap >= gapSuspectMS {
				mid := prevEnd + gap/2
				boundary := ((mid + windowMS/2) / windowMS) * windowMS
				if boundary > 0 && abs64(mid-boundary) <= nearMS {
					if _, ok := seen[boundary]; !ok {
						seen[boundary] = struct{}{}
						out = append(out, boundary)
					}
				}
			}
		}
		if u.EndTime > prevEnd {
			prevEnd = u.EndTime
		}
	}
	return out
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
