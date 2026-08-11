package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const (
	// aiSliceDefaultConcurrency 单实例内并行抢占/执行 AI 切片的 Worker 数（配置缺失时回落）。
	aiSliceDefaultConcurrency = 6
	// aiSlicePollInterval 无待处理任务时的 DB 轮询间隔（多实例兜底唤醒）。
	aiSlicePollInterval = 3 * time.Second
	// aiSliceStaleTimeout processing 超时未更新进度则自动改回 pending。
	aiSliceStaleTimeout = 20 * time.Minute
)

// aiSliceUserPromptOutputFormat 内置用户提示词中的「输出格式」固定段落。
const aiSliceUserPromptOutputFormat = `## 输出格式
- 仅输出一个 JSON 数组，包含选中的句段索引（整数），按原顺序递增。例如：[2, 5, 9, 13]。
- 索引必须来自 视频ASR 列表的行号（从 0 开始）。
- 不要输出任何额外文字、注释或角色标记。`

// AISliceWorker AI 切片任务后台调度器：通过 DB 乐观锁抢占实现多实例安全调度。
type AISliceWorker interface {
	// Enqueue 唤醒调度循环尝试领取任务（非阻塞）。
	Enqueue()
	// Process 执行已抢占（processing）的 AI 切片任务完整流程。
	Process(ctx context.Context, task *model.Task) error
	// ProcessWithOptions 执行 AI 切片阶段；可由一键成片编排复用（不 MarkCompleted）。
	ProcessWithOptions(ctx context.Context, task *model.Task, opts PhaseOptions) error
	// Start 启动后台 Worker 循环。
	Start(ctx context.Context)
}

// LLMChatClient AI 切片 / ASR 后处理所需的大模型对话接口，便于单测替换。
type LLMChatClient interface {
	Chat(ctx context.Context, messages []llm.ChatMessage) (string, error)
	// ChatStructured 用于需严格 JSON 的场景：temperature=0 + json_object，并关闭思考模式。
	ChatStructured(ctx context.Context, messages []llm.ChatMessage) (string, error)
	// ChatThinking 显式开启思考模式（AI 切片等需更深推理的场景）。
	ChatThinking(ctx context.Context, messages []llm.ChatMessage) (string, error)
}

type aiSliceWorker struct {
	taskRepo         repository.TaskRepository
	liveMaterialRepo repository.LiveMaterialRepository
	videoProjectRepo repository.VideoProjectRepository
	llmClient        LLMChatClient
	web              webroot.Config
	logger           *zap.Logger
	concurrency      int
	pollInterval     time.Duration
	staleTimeout     time.Duration

	wake      chan struct{}
	startOnce sync.Once
}

// NewAISliceWorker 创建 AI 切片后台 Worker。
// concurrency 为单实例并行 Worker 数；<=0 时使用内置默认值（6）。
// staleTimeout 为 processing 孤儿回收阈值；<=0 时使用内置默认值（20 分钟）。
// web.RootDir 非空时将 clips0 预处理前后快照落盘到 staging/{task_id}/ai_slice/。
func NewAISliceWorker(
	taskRepo repository.TaskRepository,
	liveMaterialRepo repository.LiveMaterialRepository,
	videoProjectRepo repository.VideoProjectRepository,
	llmClient LLMChatClient,
	logger *zap.Logger,
	concurrency int,
	staleTimeout time.Duration,
	web webroot.Config,
) AISliceWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	if concurrency <= 0 {
		concurrency = aiSliceDefaultConcurrency
	}
	if staleTimeout <= 0 {
		staleTimeout = aiSliceStaleTimeout
	}
	return &aiSliceWorker{
		taskRepo:         taskRepo,
		liveMaterialRepo: liveMaterialRepo,
		videoProjectRepo: videoProjectRepo,
		llmClient:        llmClient,
		web:              web,
		logger:           logger,
		concurrency:      concurrency,
		pollInterval:     aiSlicePollInterval,
		staleTimeout:     staleTimeout,
		wake:             make(chan struct{}, 1),
	}
}

// Enqueue 非阻塞唤醒调度器，Create 任务后调用以尽快领取。
func (w *aiSliceWorker) Enqueue() {
	select {
	case w.wake <- struct{}{}:
	default:
		// 已有唤醒信号在队列中，无需重复投递。
	}
}

// Start 启动若干并发领取循环 + 定时 poll，并在启动时立即回收孤儿 processing 任务。
func (w *aiSliceWorker) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		n := w.concurrency
		if n <= 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			go w.loop(ctx, i)
		}
		go w.pollLoop(ctx)
		// 启动即回收：服务重启后 processing 孤儿无需等待第一个 poll 周期。
		w.requeueStale(ctx)
		w.Enqueue()
		w.logger.Info("AI 切片 Worker 已启动",
			zap.Int("concurrency", n),
			zap.Duration("stale_timeout", w.staleTimeout),
		)
	})
}

// pollLoop 定时回收超时 processing，并唤醒领取 pending。
func (w *aiSliceWorker) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.requeueStale(ctx)
			w.Enqueue()
		}
	}
}

// requeueStale 将 updated_at 超时未刷新的 AI 切片 processing 任务改回 pending。
func (w *aiSliceWorker) requeueStale(ctx context.Context) {
	n, err := w.taskRepo.RequeueStaleProcessingByType(ctx, model.TaskTypeAISlice, w.staleTimeout)
	if err != nil {
		w.logger.Warn("回收超时 AI 切片任务失败", zap.Error(err))
		return
	}
	if n > 0 {
		w.logger.Warn("已将超时 AI 切片任务重新排队", zap.Int64("count", n))
	}
}

func (w *aiSliceWorker) loop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			w.drain(ctx, workerID)
		}
	}
}

// drain 循环抢占并处理，直到当前没有更多 pending。
func (w *aiSliceWorker) drain(ctx context.Context, workerID int) {
	for {
		if ctx.Err() != nil {
			return
		}
		task, err := w.taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
		if err != nil {
			w.logger.Error("抢占 AI 切片任务失败",
				zap.Int("worker_id", workerID),
				zap.Error(err),
			)
			return
		}
		if task == nil {
			return
		}
		w.logger.Info("已抢占 AI 切片任务",
			zap.Int("worker_id", workerID),
			zap.String("task_id", task.ID),
			zap.Int64("version", task.Version),
		)
		if err := w.Process(ctx, task); err != nil {
			w.logger.Error("AI 切片任务执行失败",
				zap.String("task_id", task.ID),
				zap.Error(err),
			)
		}
	}
}

// Process 执行单条 AI 切片完整流程（成功后 MarkCompleted）。
func (w *aiSliceWorker) Process(ctx context.Context, task *model.Task) error {
	return w.ProcessWithOptions(ctx, task, standalonePhaseOptions())
}

// ProcessWithOptions 执行 AI 切片阶段：
// 1. 读取 video_project.clips0 与 live_material.live_asr，筛选待分析句段；
// 2. 组装内置用户提示词并回写 task.usr_prompt（系统提示词已在创建时写入 task.sys_prompt）；
// 3. 调用 LLM 解析索引，合并相邻切片后写入 video_project.clips1（不覆盖 clips0）。
// opts.MarkComplete=false 时仅更新进度/ext，供一键成片继续执行草稿阶段。
func (w *aiSliceWorker) ProcessWithOptions(ctx context.Context, task *model.Task, opts PhaseOptions) error {
	if task == nil {
		return fmt.Errorf("task 不能为空")
	}
	setProgress := func(local int16) int16 {
		p := mapPhaseProgress(opts, local)
		_ = w.taskRepo.UpdateProgress(ctx, task.ID, p)
		return p
	}

	progress := setProgress(5)

	ext, err := parseTaskExt(task.Ext)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("解析任务 ext 失败: %w", err))
	}
	projectID := model.UintValue(task.VideoProjectID)
	if projectID == 0 {
		projectID = ext.VideoProjectID
	}
	if projectID == 0 {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("任务缺少 video_project_id"))
	}

	project, err := w.videoProjectRepo.GetByID(ctx, projectID)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("查询剪辑项目失败: %w", err))
	}
	if len(project.Clips0) == 0 {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("video_project.clips0 为空，无法确定待分析时间段"))
	}

	liveID := ext.LiveID
	if liveID == 0 {
		liveID = project.LiveID
	}
	if liveID == 0 {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("任务缺少 live_id"))
	}

	material, err := w.liveMaterialRepo.GetByID(ctx, liveID)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("查询直播素材失败: %w", err))
	}
	if material.ASRStatus != model.ASRStatusCompleted {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("直播素材 ASR 尚未完成"))
	}

	progress = setProgress(20)

	// clips0 预处理：排序 + 重叠并集合并；仅作本任务筛选入参，不回写 video_project.clips0。
	rawClips0 := project.Clips0
	w.logger.Info("AI 切片读取 clips0",
		zap.String("task_id", task.ID),
		zap.Uint("video_project_id", project.ID),
		zap.Int("clips0_raw_count", len(rawClips0)),
	)
	stagingDir := ""
	if strings.TrimSpace(w.web.RootDir) != "" {
		stagingDir = filepath.Join(w.web.StagingDir(task.ID), "ai_slice")
	}
	writeAISliceClips0Debug(stagingDir, "clips0_before.json", task.ID, project.ID, "before", rawClips0, w.logger)

	mergedClips0 := sortAndMergeOverlappingClipRanges(rawClips0)
	writeAISliceClips0Debug(stagingDir, "clips0_after.json", task.ID, project.ID, "after", mergedClips0, w.logger)
	w.logger.Info("AI 切片 clips0 预处理完成",
		zap.String("task_id", task.ID),
		zap.Uint("video_project_id", project.ID),
		zap.Int("before_count", len(rawClips0)),
		zap.Int("after_count", len(mergedClips0)),
		zap.Int("merged_count", len(rawClips0)-len(mergedClips0)),
		zap.String("staging_dir", stagingDir),
	)

	// 完整 ASR → 按预处理后的 clips0 时间段筛选 → 得到带下标的待分析句段列表。
	allUtterances := asr.FormatUtterancesForAPI(material.LiveASR)
	segments := filterUtterancesByClips0(allUtterances, mergedClips0)
	w.logger.Info("AI 切片按 clips0 筛选 ASR 完成",
		zap.String("task_id", task.ID),
		zap.Int("segments_count", len(segments)),
	)
	if len(segments) == 0 {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("clips0 时间段内无可用 ASR 分句"))
	}

	// 系统提示词必须在创建任务时已从 llm_system_prompt 写入；Worker 不再回退默认文案。
	sysPrompt := strings.TrimSpace(task.SysPrompt)
	if sysPrompt == "" {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("任务系统提示词为空"))
	}

	// 组装内置用户提示词，并持久化到 task.usr_prompt。
	userContent := buildAISliceUserPrompt(segments)
	if err := w.taskRepo.UpdatePrompts(ctx, task.ID, sysPrompt, userContent); err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("回写任务提示词失败: %w", err))
	}

	progress = setProgress(40)

	if w.llmClient == nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("LLM 客户端未配置"))
	}

	// AI 切片显式开启思考模式，提升复杂剪辑决策质量。
	content, err := w.llmClient.ChatThinking(ctx, []llm.ChatMessage{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userContent},
	})
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("调用大模型失败: %w", err))
	}

	progress = setProgress(70)

	// LLM 输出句段下标；越界下标在组装 clips1 时跳过。
	indices, err := llm.ParseIndices(content)
	if err != nil {
		return w.fail(ctx, task.ID, progress, err)
	}
	clips1 := buildClips1FromIndices(segments, indices)
	if clips1 == nil {
		clips1 = []model.ClipWithText{}
	}

	progress = setProgress(85)

	// 仅回写合并后的 clips1，保留用户预先配置的 clips0 分析窗口。
	project.Clips1 = clips1
	if err := w.videoProjectRepo.Update(ctx, project); err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("更新剪辑项目切片失败: %w", err))
	}

	ext.LiveID = liveID
	ext.VideoProjectID = project.ID
	extRaw, err := marshalTaskExt(ext)
	if err != nil {
		// 项目已更新，仍尽量标记完成并回写精简 ext。
		extRaw = fmt.Sprintf(`{"live_id":%d,"video_project_id":%d}`, liveID, project.ID)
	}

	if opts.MarkComplete {
		if err := w.taskRepo.MarkCompleted(ctx, task.ID, mapPhaseProgress(opts, 100), extRaw); err != nil {
			return fmt.Errorf("标记任务完成失败: %w", err)
		}
	} else {
		_ = w.taskRepo.UpdateExt(ctx, task.ID, extRaw)
		_ = setProgress(100)
	}
	w.logger.Info("AI 切片阶段完成",
		zap.String("task_id", task.ID),
		zap.Uint("video_project_id", project.ID),
		zap.Int("segments", len(segments)),
		zap.Int("indices", len(indices)),
		zap.Int("clips1", len(clips1)),
		zap.Bool("mark_complete", opts.MarkComplete),
	)
	return nil
}

func (w *aiSliceWorker) fail(ctx context.Context, taskID string, progress int16, err error) error {
	w.logger.Error("AI 切片任务失败",
		zap.String("task_id", taskID),
		zap.Int16("progress", progress),
		zap.Error(err),
	)
	if failErr := w.taskRepo.MarkFailed(ctx, taskID, progress, err.Error()); failErr != nil {
		return fmt.Errorf("AI 切片失败且写入失败状态异常: task=%w, db=%v", err, failErr)
	}
	return err
}

// parseTaskExt 解析任务 ext JSON；空字符串视为空对象。
func parseTaskExt(raw string) (TaskExt, error) {
	var ext TaskExt
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ext, nil
	}
	if err := json.Unmarshal([]byte(raw), &ext); err != nil {
		return ext, err
	}
	return ext, nil
}

// buildAISliceUserPrompt 按内置模板组装用户提示词（视频 ASR + 输出格式）。
func buildAISliceUserPrompt(segments []asr.Utterance) string {
	var b strings.Builder
	b.WriteString("## 视频ASR\n")
	for i, u := range segments {
		b.WriteString(formatASRSegmentLine(i, u))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(aiSliceUserPromptOutputFormat)
	return b.String()
}
