// Package steps 实现可插拔的剪映草稿组装步骤（调用 capcut-mate）。
// 新增能力：实现 Step 并加入 Recipe；预留步骤勿加入 DefaultRecipe（captions 已接入）。
package steps

import (
	"context"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/pkg/capcutmate"
)

// Step 草稿组装步骤。
type Step interface {
	Name() string
	Run(ctx context.Context, s *session.Session) error
}

// CapCutMateAPI 草稿生成所需的 capcut-mate 接口，便于单测替换。
type CapCutMateAPI interface {
	CreateDraft(ctx context.Context, width, height int, recordDir string) (*capcutmate.CreateDraftResponse, error)
	AddVideos(ctx context.Context, req capcutmate.AddVideosRequest, recordDir string) (*capcutmate.AddVideosResponse, error)
	AddCaptions(ctx context.Context, req capcutmate.AddCaptionsRequest, recordDir string) (*capcutmate.AddCaptionsResponse, error)
}
