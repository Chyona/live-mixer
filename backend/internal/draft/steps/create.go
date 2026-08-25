package steps

import (
	"context"
	"fmt"

	"live-mixer/internal/draft/session"

	"go.uber.org/zap"
)

// CreateStep 调用 capcut-mate create_draft。
type CreateStep struct {
	API    CapCutMateAPI
	Logger *zap.Logger
}

// Name 返回步骤名。
func (CreateStep) Name() string { return "create" }

// Run 创建空白剪映草稿并写入 Session.DraftURL。
func (st CreateStep) Run(ctx context.Context, s *session.Session) error {
	if s == nil {
		return fmt.Errorf("session 不能为空")
	}
	if st.API == nil {
		return fmt.Errorf("capcut-mate 客户端未配置")
	}
	logger := st.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	var projectID uint
	var projectW, projectH int
	if s.Project != nil {
		projectID = s.Project.ID
		projectW, projectH = s.Project.Width, s.Project.Height
	}
	logger.Info("调用 capcut-mate 创建草稿",
		zap.String("job_id", s.JobID),
		zap.Uint("video_project_id", projectID),
		zap.Int("project_width", projectW),
		zap.Int("project_height", projectH),
		zap.Int("width", s.CanvasW),
		zap.Int("height", s.CanvasH),
	)
	resp, err := st.API.CreateDraft(ctx, s.CanvasW, s.CanvasH, s.RecordDir)
	if err != nil {
		return fmt.Errorf("创建剪映草稿失败: %w", err)
	}
	s.DraftURL = resp.DraftURL
	s.ReportProgress(70)
	return nil
}
