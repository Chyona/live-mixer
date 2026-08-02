package draft

import (
	"context"

	"live-mixer/internal/model"
)

// Request 剪映草稿组装入参（与任务模型无关）。
type Request struct {
	// JobID 仅用于日志/目录命名，不强制是 task id。
	JobID    string
	Material *model.LiveMaterial
	Project  *model.VideoProject
	// Clips 为空时，若 Project 非空则从 clips1/clips0 解析并合并相邻片段。
	Clips            []model.ClipRange
	CanvasW, CanvasH int
	StagingDir       string
	RecordDir        string
	// Progress 可选：本地进度 0–100；由任务层映射到 task.progress。
	Progress func(local int16)
	// Recipe 为空时使用 DefaultRecipe。
	Recipe Recipe
}

// Result 草稿组装产出。
type Result struct {
	DraftURL    string
	ClipsTarURL string // 切片 tar 包下载地址；打包/上传失败时为空
}

// Generator 纯草稿组装能力：给定素材与 clips，产出 draft_url（及可选 clips_tar_url）。
type Generator interface {
	Build(ctx context.Context, req Request) (*Result, error)
}
