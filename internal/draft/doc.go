// Package draft 实现剪映草稿纯组装：素材准备 + 可插拔 Step 管道。
// 任务调度（claim/progress/complete）由 service 层适配，本包不依赖 TaskRepository。
//
// 扩展约定：
//  1. 在 internal/pkg/capcutmate 增加对应 API 客户端方法；
//  2. 在 internal/draft/steps 实现 Step；
//  3. 将步骤加入 Recipe（默认 DefaultRecipe = create + videos + captions）。
//
// add_videos 的视频 URL：本地切片经 ObjectUploader 上传对象存储后使用返回的公网地址。
// add_captions 的字幕：来自 live_material.live_asr，按 VideosStep 的 ClipPlacement 映射到草稿时间轴，与视频切片同步。
package draft

import (
	"live-mixer/internal/draft/steps"
)

// 重新导出常用步骤接口，便于外部注入 mock。
type (
	// CapCutMateAPI 草稿生成所需的 capcut-mate 接口。
	CapCutMateAPI = steps.CapCutMateAPI
	// ObjectUploader 将本地切片上传到对象存储。
	ObjectUploader = steps.ObjectUploader
	// Step 草稿组装步骤。
	Step = steps.Step
)
