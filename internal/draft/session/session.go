// Package session 定义剪映草稿组装管道的共享上下文。
// 作为叶子包供 draft / prepare / steps 引用，避免循环依赖。
package session

import (
	"live-mixer/internal/model"
)

// Session 贯穿 Prepare 与各 Step 的可变上下文。
type Session struct {
	// JobID 仅用于日志与目录命名，不绑定任务模型。
	JobID      string
	Project    *model.VideoProject
	Material   *model.LiveMaterial
	StagingDir string
	RecordDir  string
	SourcePath string
	ClipPaths  []string
	Clips      []model.ClipRange
	DraftURL   string
	CanvasW    int
	CanvasH    int
	Timeline   *Timeline
	// Progress 可选：报告本地进度（0-100），由调用方映射。
	Progress func(local int16)
}

// ReportProgress 安全调用进度回调。
func (s *Session) ReportProgress(local int16) {
	if s != nil && s.Progress != nil {
		s.Progress(local)
	}
}
