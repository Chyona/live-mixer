package draft

import (
	"live-mixer/internal/draft/steps"

	"go.uber.org/zap"
)

// Recipe 有序组装步骤列表。
type Recipe struct {
	Steps []steps.Step
}

// DefaultRecipe 默认流水线：创建草稿 → 上传切片并添加主视频轨 → 按 ASR 添加同步字幕。
func DefaultRecipe(api CapCutMateAPI, uploader steps.ObjectUploader, logger *zap.Logger) Recipe {
	return Recipe{
		Steps: []steps.Step{
			steps.CreateStep{API: api, Logger: logger},
			steps.VideosStep{API: api, Uploader: uploader, Logger: logger},
			steps.CaptionsStep{API: api, Logger: logger},
		},
	}
}
