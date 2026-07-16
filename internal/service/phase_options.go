package service

// PhaseOptions 控制子阶段在编排任务中的进度映射与完成语义。
// 本地进度 local（0-100）映射为：ProgressBase + local * ProgressSpan / 100。
type PhaseOptions struct {
	// MarkComplete 为 true 时阶段成功后调用 MarkCompleted；编排中间阶段应设为 false。
	MarkComplete bool
	// ProgressBase 映射后的进度起点（一键成片中切片阶段为 0，草稿阶段为 50）。
	ProgressBase int16
	// ProgressSpan 映射后的进度跨度；<=0 时视为 100。
	ProgressSpan int16
}

// standalonePhaseOptions 独立任务（ai_slice / draft）的默认阶段选项。
func standalonePhaseOptions() PhaseOptions {
	return PhaseOptions{MarkComplete: true, ProgressBase: 0, ProgressSpan: 100}
}

// mapPhaseProgress 将阶段内本地进度（0-100）映射到父任务进度。
func mapPhaseProgress(opts PhaseOptions, local int16) int16 {
	span := opts.ProgressSpan
	if span <= 0 {
		span = 100
	}
	if local < 0 {
		local = 0
	}
	if local > 100 {
		local = 100
	}
	return opts.ProgressBase + int16(int(span)*int(local)/100)
}
