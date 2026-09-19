package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const (
	// liveIngestCleanupMaxIDDirsPerRun 每轮最多整棵删除的素材 ID 目录数，避免磁盘 IO 打满。
	liveIngestCleanupMaxIDDirsPerRun = 2
	// liveIngestCleanupMaxEpochDirsPerRun 每轮最多删除的旧代数目录数。
	liveIngestCleanupMaxEpochDirsPerRun = 2000
)

// liveIngestMaterialLookup 清理任务所需的素材查询（便于单测注入）。
type liveIngestMaterialLookup interface {
	ListByIDs(ctx context.Context, ids []uint) (map[uint]*model.LiveMaterial, error)
}

// LiveIngestCleanupResult 一次 live_ingest 清理的统计。
type LiveIngestCleanupResult struct {
	Scanned      int
	Protected    int
	RemovedIDs   int
	RemovedEpoch int
	Kept         int
}

// CleanupLiveIngest 清理 staging/live_ingest：
// 1) 一级素材 ID 目录按 mtime 保留最多 keep 个；进行中 / ASR processing 永不删；
// 2) 对仍保留的目录，删除 n < ingest_epoch-1 且无未上传本地窗/master 的旧代数。
func CleanupLiveIngest(
	ctx context.Context,
	rootDir string,
	keep int,
	lookup liveIngestMaterialLookup,
	logger *zap.Logger,
) (LiveIngestCleanupResult, error) {
	var result LiveIngestCleanupResult
	if strings.TrimSpace(rootDir) == "" {
		return result, fmt.Errorf("rootDir 为空")
	}
	if keep <= 0 {
		return result, fmt.Errorf("keep 必须为正：%d", keep)
	}
	if lookup == nil {
		return result, fmt.Errorf("素材查询未配置")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	ingestRoot := filepath.Join(rootDir, "staging", webroot.LiveIngestSubDir)
	entries, err := os.ReadDir(ingestRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("读取 live_ingest 失败: %w", err)
	}

	type idDir struct {
		id      uint
		path    string
		modTime time.Time
	}
	dirs := make([]idDir, 0, len(entries))
	ids := make([]uint, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, ok := parseLiveIngestMaterialID(entry.Name())
		if !ok {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			logger.Warn("读取 live_ingest 素材目录信息失败",
				zap.String("name", entry.Name()),
				zap.Error(infoErr),
			)
			continue
		}
		dirs = append(dirs, idDir{
			id:      id,
			path:    filepath.Join(ingestRoot, entry.Name()),
			modTime: info.ModTime(),
		})
		ids = append(ids, id)
	}
	result.Scanned = len(dirs)
	if len(dirs) == 0 {
		return result, nil
	}

	materials, err := lookup.ListByIDs(ctx, ids)
	if err != nil {
		return result, fmt.Errorf("批量查询素材失败: %w", err)
	}

	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].modTime.Equal(dirs[j].modTime) {
			return dirs[i].id > dirs[j].id
		}
		return dirs[i].modTime.After(dirs[j].modTime)
	})

	protected := make(map[uint]bool, len(dirs))
	for _, d := range dirs {
		if liveIngestDirProtected(materials[d.id]) {
			protected[d.id] = true
			result.Protected++
		}
	}

	// 配额：从最旧可淘汰目录整棵删除，直到总数 ≤ keep 或本轮名额用尽。
	if len(dirs) > keep {
		need := len(dirs) - keep
		if need > liveIngestCleanupMaxIDDirsPerRun {
			need = liveIngestCleanupMaxIDDirsPerRun
		}
		for i := len(dirs) - 1; i >= 0 && need > 0; i-- {
			d := dirs[i]
			if protected[d.id] {
				continue
			}
			if rmErr := os.RemoveAll(d.path); rmErr != nil {
				logger.Warn("删除 live_ingest 素材目录失败",
					zap.Uint("material_id", d.id),
					zap.String("path", d.path),
					zap.Error(rmErr),
				)
				continue
			}
			logger.Info("已删除过期 live_ingest 素材目录",
				zap.Uint("material_id", d.id),
				zap.String("path", d.path),
			)
			result.RemovedIDs++
			need--
			dirs = append(dirs[:i], dirs[i+1:]...)
			delete(materials, d.id)
			delete(protected, d.id)
		}
	}
	result.Kept = len(dirs)

	// 旧代数裁剪（含进行中保留目录）。
	epochBudget := liveIngestCleanupMaxEpochDirsPerRun
	for _, d := range dirs {
		if epochBudget <= 0 {
			break
		}
		mat := materials[d.id]
		removed, remErr := pruneLiveIngestOldEpochs(d.path, mat, &epochBudget)
		result.RemovedEpoch += removed
		if remErr != nil {
			logger.Warn("裁剪 live_ingest 旧代数失败",
				zap.Uint("material_id", d.id),
				zap.Error(remErr),
			)
		}
	}

	return result, nil
}

func parseLiveIngestMaterialID(name string) (uint, bool) {
	if name == "" {
		return 0, false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(name, 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint(n), true
}

func liveIngestDirProtected(m *model.LiveMaterial) bool {
	if m == nil {
		return false
	}
	if m.ASRStatus == model.ASRStatusProcessing {
		return true
	}
	switch m.LiveStatus {
	case model.LiveStatusWaiting, model.LiveStatusConnecting, model.LiveStatusLive, model.LiveStatusEnding:
		return true
	default:
		return false
	}
}

// pruneLiveIngestOldEpochs 删除 n < ingest_epoch-1 且无未上传本地媒体的代数目录。
// material 为 nil（库中不存在）时不裁代数——整棵 ID 应由配额删除；若仍保留则保守跳过。
func pruneLiveIngestOldEpochs(materialDir string, material *model.LiveMaterial, budget *int) (removed int, err error) {
	if material == nil || budget == nil || *budget <= 0 {
		return 0, nil
	}
	keepMinEpoch := material.IngestEpoch - 1
	if keepMinEpoch < 1 {
		keepMinEpoch = 1
	}
	entries, readErr := os.ReadDir(materialDir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return 0, nil
		}
		return 0, readErr
	}

	windowsByIndex := map[int]model.MediaWindow{}
	for _, w := range material.ParsedMediaWindows() {
		windowsByIndex[w.Index] = w
	}
	hasMasterURL := strings.TrimSpace(material.MasterMP4URL()) != "" ||
		(strings.TrimSpace(material.LiveURL) != "" && !model.IsProbablyM3U8URL(material.LiveURL))

	var firstErr error
	for _, entry := range entries {
		if *budget <= 0 {
			break
		}
		if !entry.IsDir() {
			continue
		}
		epoch, ok := parseLiveIngestEpochDir(entry.Name())
		if !ok || epoch >= keepMinEpoch {
			continue
		}
		epochPath := filepath.Join(materialDir, entry.Name())
		if epochHasUnuploadedLocalMedia(epochPath, windowsByIndex, hasMasterURL) {
			continue
		}
		if rmErr := os.RemoveAll(epochPath); rmErr != nil {
			if firstErr == nil {
				firstErr = rmErr
			}
			continue
		}
		removed++
		*budget--
	}
	return removed, firstErr
}

func parseLiveIngestEpochDir(name string) (int64, bool) {
	if !strings.HasPrefix(name, "e") {
		return 0, false
	}
	n, err := strconv.ParseInt(name[1:], 10, 64)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

func epochHasUnuploadedLocalMedia(epochDir string, windowsByIndex map[int]model.MediaWindow, hasMasterURL bool) bool {
	masterPath := filepath.Join(epochDir, liveingest.MasterMP4FileName())
	if st, err := os.Stat(masterPath); err == nil && st.Size() > 0 && !hasMasterURL {
		return true
	}
	winDir := filepath.Join(epochDir, liveingest.WindowsDirName())
	matches, _ := filepath.Glob(filepath.Join(winDir, "window_*.mp4"))
	for _, p := range matches {
		st, err := os.Stat(p)
		if err != nil || st.Size() == 0 {
			continue
		}
		idx, ok := parseWindowMP4Index(filepath.Base(p))
		if !ok {
			// 无法解析的本地窗文件：保守保留。
			return true
		}
		meta, found := windowsByIndex[idx]
		if !found || strings.TrimSpace(meta.URL) == "" {
			return true
		}
	}
	return false
}

func parseWindowMP4Index(base string) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(base, "window_%d.mp4", &n); err != nil {
		return 0, false
	}
	return n, true
}

// Ensure LiveMaterialRepository satisfies lookup used by cleanup (compile-time check).
var _ liveIngestMaterialLookup = (repository.LiveMaterialRepository)(nil)
