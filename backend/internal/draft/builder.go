package draft

import (
	"context"
	"fmt"

	"live-mixer/internal/draft/prepare"
	"live-mixer/internal/draft/session"
	"live-mixer/internal/draft/steps"
	"live-mixer/internal/model"

	"go.uber.org/zap"
)

// Builder 执行素材准备 + Recipe 步骤，实现 Generator。
type Builder struct {
	Prepare  *prepare.Pipeline
	API      CapCutMateAPI
	Uploader steps.ObjectUploader
	Logger   *zap.Logger
}

// NewBuilder 创建草稿组装器。
// uploader 用于将本地切片上传到对象存储，供 add_videos 使用公网 URL。
func NewBuilder(prep *prepare.Pipeline, api CapCutMateAPI, uploader steps.ObjectUploader, logger *zap.Logger) *Builder {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Builder{Prepare: prep, API: api, Uploader: uploader, Logger: logger}
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
	clipTexts := resolveAlignedClipTexts(req, clips, b.Logger)
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
		recipe = DefaultRecipe(b.API, b.Uploader, b.Logger)
	}

	s := &session.Session{
		JobID:          req.JobID,
		Project:        req.Project,
		Material:       req.Material,
		StagingDir:     req.StagingDir,
		RecordDir:      req.RecordDir,
		LocalIngestDir: req.LocalIngestDir,
		Clips:          clips,
		ClipTexts:      clipTexts,
		CanvasW:        req.CanvasW,
		CanvasH:        req.CanvasH,
		Timeline:       session.NewTimeline(),
		Progress:       req.Progress,
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

	result := &Result{DraftURL: s.DraftURL}

	// 方案 A：写出 caption_diag.json（clip want/actual/delta + caption map_err），上传供留存；失败不阻断草稿。
	diagPath, diagURL, diagErr := steps.WriteAndUploadCaptionDiag(ctx, s, b.Uploader, nil, b.Logger)
	if diagErr != nil {
		b.Logger.Warn("字幕对齐诊断报告失败",
			zap.String("job_id", s.JobID),
			zap.Error(diagErr),
		)
	} else {
		result.CaptionDiagURL = diagURL
	}

	// 将本地 clip_XXX.mp4（及可选 caption_diag.json）打成 {jobID}.tar 并上传；失败仅记日志，不阻断草稿成功。
	packPaths := append([]string(nil), s.ClipPaths...)
	if diagPath != "" {
		packPaths = append(packPaths, diagPath)
	}
	clipsTarURL, err := PackAndUploadClipsTar(ctx, b.Uploader, s.StagingDir, s.JobID, packPaths)
	if err != nil {
		b.Logger.Error("切片 tar 打包上传失败",
			zap.String("job_id", s.JobID),
			zap.Error(err),
		)
	} else {
		result.ClipsTarURL = clipsTarURL
	}
	return result, nil
}

// resolveAlignedClipTexts 取 video_project.clips1，按同一合并规则与切片区间对齐。
// 对齐成功返回带 text/words 的切片（字幕据此生成，人工删除的文字不会再回到字幕）；
// 未对齐（如显式传入 clips、clips1 与区间不一致）返回 nil，字幕回退 live_asr 映射。
func resolveAlignedClipTexts(req Request, clips []model.ClipRange, logger *zap.Logger) []model.ClipWithText {
	if req.Project == nil || len(req.Project.Clips1) == 0 {
		return nil
	}
	merged := prepare.MergeAdjacentClipTexts(req.Project.Clips1, prepare.ClipMergeGapMS)
	if !prepare.ClipTextsMatchRanges(clips, merged) {
		logger.Warn("clips1 与切片区间不一致，字幕回退 ASR 时间轴",
			zap.String("job_id", req.JobID),
			zap.Int("clip_count", len(clips)),
			zap.Int("clip_text_count", len(merged)),
		)
		return nil
	}
	return merged
}

// 确保 steps 包被引用（DefaultRecipe 已引用）。
var _ steps.Step = steps.CreateStep{}
var _ Generator = (*Builder)(nil)
