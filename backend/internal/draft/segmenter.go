package draft

import "context"

// CaptionSegmenter 为切片文案提供字幕行：标点优先断句，超长小句再由 LLM 报切点位置。
// 实现见 service.CaptionSegmenter；接口定义在 draft 包内，避免 draft → service 的循环依赖。
type CaptionSegmenter interface {
	// SegmentTexts 返回与输入等长的结果切片，元素为 nil 表示该条没有用上模型结果（调用方按规则折行）。
	// 实现必须自行保证：不返回错误、不阻塞过久、不改动原文内容（只允许丢掉行首尾标点与空白）。
	SegmentTexts(ctx context.Context, texts []string) [][]string
}
