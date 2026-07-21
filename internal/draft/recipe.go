package draft

import (
	"live-mixer/internal/draft/steps"

	"go.uber.org/zap"
)

// Recipe 有序组装步骤列表。
type Recipe struct {
	Steps []steps.Step
}

// DefaultRecipe 兼容现状：创建草稿 + 添加主视频轨。
func DefaultRecipe(api CapCutMateAPI, logger *zap.Logger) Recipe {
	return Recipe{
		Steps: []steps.Step{
			steps.CreateStep{API: api, Logger: logger},
			steps.VideosStep{API: api, Logger: logger},
		},
	}
}
