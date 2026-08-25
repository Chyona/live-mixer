package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"live-mixer/internal/model"

	"go.uber.org/zap"
)

// aiSliceClips0DebugRecord 写入 staging 的 clips0 预处理调试记录。
type aiSliceClips0DebugRecord struct {
	RecordedAt      string            `json:"recorded_at"`
	TaskID          string            `json:"task_id"`
	VideoProjectID  uint              `json:"video_project_id"`
	Phase           string            `json:"phase"`
	Count           int               `json:"count"`
	Clips           []model.ClipRange `json:"clips"`
}

// writeAISliceClips0Debug 将 clips0 处理前/后快照落盘到 dir/name。
// dir 为空时 noop；写失败只打 Warn，不中断任务。
func writeAISliceClips0Debug(
	dir string,
	name string,
	taskID string,
	videoProjectID uint,
	phase string,
	clips []model.ClipRange,
	logger *zap.Logger,
) {
	if logger == nil {
		logger = zap.NewNop()
	}
	dir = strings.TrimSpace(dir)
	name = strings.TrimSpace(name)
	if dir == "" || name == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("创建 AI 切片调试目录失败",
			zap.String("dir", dir),
			zap.Error(err),
		)
		return
	}
	if clips == nil {
		clips = []model.ClipRange{}
	}
	payload := aiSliceClips0DebugRecord{
		RecordedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		TaskID:         taskID,
		VideoProjectID: videoProjectID,
		Phase:          phase,
		Count:          len(clips),
		Clips:          clips,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		logger.Warn("序列化 AI 切片 clips0 调试数据失败",
			zap.String("name", name),
			zap.Error(err),
		)
		return
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		logger.Warn("写入 AI 切片 clips0 调试文件失败",
			zap.String("path", path),
			zap.Error(err),
		)
	}
}
