package draft

import (
	"context"
	"fmt"

	"live-mixer/internal/draft/prepare"
	"live-mixer/internal/draft/session"
	"live-mixer/internal/draft/steps"

	"go.uber.org/zap"
)

// Builder 执行素材准备 + Recipe 步骤，实现 Generator。
type Builder struct {
	Prepare *prepare.Pipeline
	API     CapCutMateAPI
	Logger  *zap.Logger
}

// NewBuilder 创建草稿组装器。
func NewBuilder(prep *prepare.Pipeline, api CapCutMateAPI, logger *zap.Logger) *Builder {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Builder{Prepare: prep, API: api, Logger: logger}
}

// Build 解析 clips → Prepare → 按 Recipe 执行 Steps，返回 draft_url。
func (b *Builder) Build(ctx context.Context, req Request) (*Result, error) {
	if b == nil {
		return nil, fmt.Errorf("builder 未配置")
	}
	if b.Prepare == nil {
		return nil, fmt.Errorf("prepare 流水线未配置")
	}
	if req.Material == nil {
		return nil, fmt.Errorf("material 不能为空")
	}
	if req.StagingDir == "" || req.RecordDir == "" {
		return nil, fmt.Errorf("StagingDir/RecordDir 不能为空")
	}

	clips := req.Clips
	if len(clips) == 0 {
		if req.Project == nil {
			return nil, fmt.Errorf("clips 为空且未提供 project")
		}
		var err error
		clips, err = prepare.ResolveClipRanges(req.Project)
		if err != nil {
			return nil, err
		}
	}
	beforeMerge := len(clips)
	clips = prepare.MergeAdjacentClipRanges(clips, prepare.ClipMergeGapMS)
	b.Logger.Info("草稿裁剪前合并相邻片段",
		zap.String("job_id", req.JobID),
		zap.Int("clips_before", beforeMerge),
		zap.Int("clips_after", len(clips)),
		zap.Int64("merge_gap_ms", prepare.ClipMergeGapMS),
	)
	if err := prepare.ValidateClipRanges(clips); err != nil {
		return nil, err
	}

	recipe := req.Recipe
	if len(recipe.Steps) == 0 {
		recipe = DefaultRecipe(b.API, b.Logger)
	}

	s := &session.Session{
		JobID:      req.JobID,
		Project:    req.Project,
		Material:   req.Material,
		StagingDir: req.StagingDir,
		RecordDir:  req.RecordDir,
		Clips:      clips,
		CanvasW:    req.CanvasW,
		CanvasH:    req.CanvasH,
		Web:        req.Web,
		Timeline:   session.NewTimeline(),
		Progress:   req.Progress,
	}

	if err := b.Prepare.Run(ctx, s); err != nil {
		return nil, err
	}
	for _, step := range recipe.Steps {
		if step == nil {
			continue
		}
		b.Logger.Info("执行草稿组装步骤",
			zap.String("job_id", s.JobID),
			zap.String("step", step.Name()),
		)
		if err := step.Run(ctx, s); err != nil {
			return nil, fmt.Errorf("步骤 %s 失败: %w", step.Name(), err)
		}
	}
	if s.DraftURL == "" {
		return nil, fmt.Errorf("草稿组装完成但 draft_url 为空")
	}
	return &Result{DraftURL: s.DraftURL}, nil
}

// 确保 steps 包被引用（DefaultRecipe 已引用）。
var _ steps.Step = steps.CreateStep{}
var _ Generator = (*Builder)(nil)
