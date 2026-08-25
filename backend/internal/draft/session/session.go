// Package session 定义剪映草稿组装管道的共享上下文。
// 作为叶子包供 draft / prepare / steps 引用，避免循环依赖。
package session

import (
	"live-mixer/internal/model"
)

// ClipPlacement 描述一段 add_videos 切片在源素材与草稿时间轴上的对应关系。
// 字幕步骤据此把 ASR（源时间轴，毫秒）映射到草稿轨道（微秒），保证与视频切片同步。
type ClipPlacement struct {
	// SourceStartMS / SourceEndMS 源视频时间范围（毫秒），与 Session.Clips 一致。
	SourceStartMS int64
	SourceEndMS   int64
	// DraftStartUS / DraftEndUS 草稿主时间轴位置（微秒），与 add_videos 的 start/end 一致。
	DraftStartUS int64
	DraftEndUS   int64
}

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
	// ClipPlacements 由 VideosStep 写入：每段切片在源时间轴与草稿时间轴的映射，供字幕同步。
	ClipPlacements []ClipPlacement
	DraftURL       string
	CanvasW        int
	CanvasH        int
	Timeline       *Timeline
	// Progress 可选：报告本地进度（0-100），由调用方映射。
	Progress func(local int16)
}

// ReportProgress 安全调用进度回调。
func (s *Session) ReportProgress(local int16) {
	if s != nil && s.Progress != nil {
		s.Progress(local)
	}
}
