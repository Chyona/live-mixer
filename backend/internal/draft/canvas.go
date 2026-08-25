package draft

import (
	"live-mixer/internal/model"
)

const (
	// DefaultCanvasWidth / DefaultCanvasHeight 剪映草稿默认画布尺寸（竖屏）。
	DefaultCanvasWidth  = 1080
	DefaultCanvasHeight = 1920
)

// ResolveCanvasSize 解析草稿画布尺寸：
// 请求参数优先 → video_project.width/height → 内置默认值。
func ResolveCanvasSize(reqWidth, reqHeight int, project *model.VideoProject) (int, int) {
	width, height := reqWidth, reqHeight
	if width <= 0 && project != nil {
		width = project.Width
	}
	if height <= 0 && project != nil {
		height = project.Height
	}
	if width <= 0 {
		width = DefaultCanvasWidth
	}
	if height <= 0 {
		height = DefaultCanvasHeight
	}
	return width, height
}
