package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"

	"go.uber.org/zap"
)

// sealReadyMediaWindows 按媒体窗时长封窗：合成 window_N.mp4（+同源 TS），更新 media_windows 与预览 playlist。
// forcePartial：关播时即使未满一窗也封最后一段。
func (w *liveIngestWorker) sealReadyMediaWindows(
	ctx context.Context,
	material *model.LiveMaterial,
	segDurMS map[int64]int64,
	forcePartial bool,
) error {
	if material == nil || material.NextSeg <= 0 {
		return nil
	}
	windowMS := material.EffectiveMediaWindowMS()
	workDir := w.segmentDir(material)
	winDir := filepath.Join(workDir, "windows")
	if err := os.MkdirAll(winDir, 0o755); err != nil {
		return err
	}
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	if nominal <= 0 {
		nominal = 6000
	}

	for {
		segStart := material.NextWindowSeg
		if segStart < 0 {
			segStart = 0
		}
		if segStart >= material.NextSeg {
			break
		}

		var sumMS int64
		segEnd := segStart
		for segEnd < material.NextSeg {
			ms := segDurMS[segEnd]
			if ms <= 0 {
				ms = nominal
			}
			// 已有内容且再加一片会明显超过窗长、且已接近满窗：在边界封口。
			if sumMS > 0 && sumMS >= windowMS*9/10 && sumMS+ms > windowMS {
				break
			}
			sumMS += ms
			segEnd++
			if sumMS >= windowMS {
				break
			}
		}
		if segEnd <= segStart {
			break
		}
		partial := sumMS < windowMS
		if partial && !forcePartial {
			break
		}

		windows := material.ParsedMediaWindows()
		winIdx := 0
		if len(windows) > 0 {
			winIdx = windows[len(windows)-1].Index + 1
		}
		startMS := windows.TotalReadyMS()
		files := make([]string, 0, segEnd-segStart)
		for i := segStart; i < segEnd; i++ {
			p := filepath.Join(workDir, liveingest.SegmentFileName(int(i)))
			if st, err := os.Stat(p); err != nil || st.Size() == 0 {
				return fmt.Errorf("封窗缺少分片 seg=%d path=%s", i, p)
			}
			files = append(files, p)
		}
		mp4Path := filepath.Join(winDir, liveingest.WindowMP4FileName(winIdx))
		tsPath := filepath.Join(winDir, liveingest.WindowTSFileName(winIdx))

		w.logger.Info("开始合成媒体窗",
			zap.Uint("material_id", material.ID),
			zap.Int("window_index", winIdx),
			zap.Int64("seg_start", segStart),
			zap.Int64("seg_end", segEnd),
			zap.Int64("est_dur_ms", sumMS),
			zap.Bool("partial", partial),
			zap.String("mp4", mp4Path),
		)
		if err := w.ffmpeg.ConcatMediaFiles(ctx, files, mp4Path); err != nil {
			return fmt.Errorf("合成媒体窗 mp4 失败: %w", err)
		}
		durMS := sumMS
		if w.prober != nil {
			if tl, err := w.prober.ProbeMediaTimeline(ctx, mp4Path); err == nil && tl.FormatDurationSec > 0 {
				if probed := int64(tl.FormatDurationSec * 1000); probed > 0 {
					durMS = probed
				}
			}
		}
		if err := w.ffmpeg.RemuxToMPEGTS(ctx, mp4Path, tsPath); err != nil {
			return fmt.Errorf("合成媒体窗 ts 失败: %w", err)
		}

		mp4Key := liveingest.WindowMP4ObjectKey(material.RecordUUID, winIdx)
		tsKey := liveingest.WindowTSObjectKey(material.RecordUUID, winIdx)
		var mp4URL, tsURL string
		if w.storage != nil {
			var err error
			mp4URL, err = w.storage.UploadFile(ctx, mp4Path, mp4Key)
			if err != nil {
				return fmt.Errorf("上传媒体窗 mp4 失败: %w", err)
			}
			tsURL, err = w.storage.UploadFile(ctx, tsPath, tsKey)
			if err != nil {
				return fmt.Errorf("上传媒体窗 ts 失败: %w", err)
			}
		}

		wmeta := model.MediaWindow{
			Index:     winIdx,
			StartMS:   startMS,
			EndMS:     startMS + durMS,
			DurMS:     durMS,
			URL:       mp4URL,
			TSURL:     tsURL,
			ObjectKey: mp4Key,
			TSKey:     tsKey,
			SegStart:  segStart,
			SegEnd:    segEnd,
			Ready:     true,
		}
		windows = windows.Upsert(wmeta)
		material.MediaWindows = windows.Marshal()
		material.NextWindowSeg = segEnd

		endedPlaylist := forcePartial && material.NextWindowSeg >= material.NextSeg
		playlistURL, plErr := w.publishWindowsPlaylist(ctx, material, endedPlaylist)
		if plErr != nil {
			w.logger.Warn("发布媒体窗播放列表失败", zap.Uint("material_id", material.ID), zap.Error(plErr))
		}
		if playlistURL != "" {
			material.RecordPlaylistURL = playlistURL
		}
		if err := w.repo.CommitMediaWindow(ctx, material.ID, material.IngestEpoch, material.MediaWindows, material.NextWindowSeg, material.Duration, playlistURL, true); err != nil {
			return fmt.Errorf("写回媒体窗失败: %w", err)
		}
		material.ASRDue = true
		w.logger.Info("媒体窗已就绪",
			zap.Uint("material_id", material.ID),
			zap.Int("window_index", winIdx),
			zap.Int64("start_ms", wmeta.StartMS),
			zap.Int64("end_ms", wmeta.EndMS),
			zap.Int64("dur_ms", wmeta.DurMS),
			zap.String("mp4_url", mp4URL),
		)
		enqueueWake(w.asrWake, 1)

		if !forcePartial {
			break
		}
		if material.NextWindowSeg >= material.NextSeg {
			break
		}
	}
	return nil
}

func (w *liveIngestWorker) publishWindowsPlaylist(ctx context.Context, material *model.LiveMaterial, ended bool) (string, error) {
	if w.storage == nil {
		return "", fmt.Errorf("对象存储未配置")
	}
	windows := material.ParsedMediaWindows()
	for i := range windows {
		if !windows[i].Ready {
			continue
		}
		if strings.TrimSpace(windows[i].TSURL) == "" && strings.TrimSpace(windows[i].TSKey) != "" {
			if u, err := w.storage.AccessURL(ctx, windows[i].TSKey); err == nil {
				windows[i].TSURL = u
			}
		}
		if strings.TrimSpace(windows[i].URL) == "" && strings.TrimSpace(windows[i].ObjectKey) != "" {
			if u, err := w.storage.AccessURL(ctx, windows[i].ObjectKey); err == nil {
				windows[i].URL = u
			}
		}
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Index < windows[j].Index })
	td := int(material.EffectiveMediaWindowMS() / 1000)
	if td <= 0 {
		td = int(model.LiveMediaWindowDuration / time.Second)
	}
	body := liveingest.BuildWindowsPlaylist(windows, td, ended)
	tmp := filepath.Join(w.segmentDir(material), "live.m3u8")
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	uploaded, err := w.storage.UploadFile(ctx, tmp, liveingest.PlaylistObjectKey(material.RecordUUID))
	if err != nil {
		return "", err
	}
	return liveingest.PreferStablePlaylistURL(material.RecordPlaylistURL, uploaded), nil
}

// resolveWindowMP4Path 优先本地窗文件，否则从对象存储拉到本地。
func (w *liveIngestWorker) resolveWindowMP4Path(ctx context.Context, material *model.LiveMaterial, win model.MediaWindow) (string, error) {
	workDir := w.segmentDir(material)
	local := filepath.Join(workDir, "windows", liveingest.WindowMP4FileName(win.Index))
	if st, err := os.Stat(local); err == nil && st.Size() > 0 {
		return local, nil
	}
	_ = os.MkdirAll(filepath.Dir(local), 0o755)
	url := strings.TrimSpace(win.URL)
	if url == "" && w.storage != nil && strings.TrimSpace(win.ObjectKey) != "" {
		u, err := w.storage.AccessURL(ctx, win.ObjectKey)
		if err != nil {
			return "", err
		}
		url = u
	}
	if url == "" {
		return "", fmt.Errorf("媒体窗 %d 无可用 URL", win.Index)
	}
	if err := downloadHTTPFile(ctx, w.httpClient, url, local); err != nil {
		return "", fmt.Errorf("下载媒体窗 mp4 失败: %w", err)
	}
	return local, nil
}
