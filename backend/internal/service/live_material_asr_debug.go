package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// asrDebugRecorder 将 ASR 处理过程调试数据落盘到 staging；dir 为空时全部 noop。
type asrDebugRecorder struct {
	dir    string
	logger *zap.Logger
}

func newASRDebugRecorder(dir string, logger *zap.Logger) *asrDebugRecorder {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &asrDebugRecorder{
		dir:    strings.TrimSpace(dir),
		logger: logger,
	}
}

// Write 将 payload 以缩进 JSON 写入 dir/name；失败只打 Warn，不返回错误。
func (r *asrDebugRecorder) Write(name string, payload any) {
	if r == nil || r.dir == "" || strings.TrimSpace(name) == "" {
		return
	}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		r.logger.Warn("创建 ASR 调试目录失败",
			zap.String("dir", r.dir),
			zap.Error(err),
		)
		return
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		r.logger.Warn("序列化 ASR 调试数据失败",
			zap.String("name", name),
			zap.Error(err),
		)
		return
	}
	path := filepath.Join(r.dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		r.logger.Warn("写入 ASR 调试文件失败",
			zap.String("path", path),
			zap.Error(err),
		)
	}
}

func asrDebugRecordedAt() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
