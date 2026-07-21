// Package draft 实现剪映草稿纯组装：素材准备 + 可插拔 Step 管道。
// 任务调度（claim/progress/complete）由 service 层适配，本包不依赖 TaskRepository。
//
// 扩展约定：
//  1. 在 internal/pkg/capcutmate 增加对应 API 客户端方法；
//  2. 在 internal/draft/steps 实现 Step；
//  3. 将步骤加入 Recipe（默认 DefaultRecipe = create + videos）。
package draft

import (
	"live-mixer/internal/draft/steps"
)

// 重新导出常用步骤接口，便于外部注入 mock。
type (
	// CapCutMateAPI 草稿生成所需的 capcut-mate 接口。
	CapCutMateAPI = steps.CapCutMateAPI
	// Step 草稿组装步骤。
	Step = steps.Step
)
