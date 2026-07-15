package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const (
	// aiSliceDefaultConcurrency 单实例内并行抢占/执行 AI 切片的 Worker 数。
	aiSliceDefaultConcurrency = 2
	// aiSlicePollInterval 无待处理任务时的 DB 轮询间隔（多实例兜底唤醒）。
	aiSlicePollInterval = 3 * time.Second
	// aiSliceDefaultSysPrompt 未指定系统提示词时的默认指令。
	aiSliceDefaultSysPrompt = `你是直播高光切片助手。根据用户提供的 ASR 分句（含毫秒级时间戳）与目标成片时长，选出最适合成片的高光片段。
只输出 JSON，不要输出其它说明文字。格式严格为：
{"clips":[{"start_time":毫秒整数,"end_time":毫秒整数},...]}
要求：
1. start_time < end_time，且时间落在 ASR 覆盖范围内；
2. 各片段尽量不重叠；
3. 所有片段合计时长尽量接近目标时长；
4. 优先选择话术完整、情绪高、转化感强的片段。`
)

// AISliceWorker AI 切片任务后台调度器：通过 DB 原子抢占实现多实例安全调度。
type AISliceWorker interface {
	// Enqueue 唤醒调度循环尝试领取任务（非阻塞）。
	Enqueue()
	// Process 执行已抢占（processing）的 AI 切片任务完整流程。
	Process(ctx context.Context, task *model.Task) error
	// Start 启动后台 Worker 循环。
	Start(ctx context.Context)
}

// LLMChatClient AI 切片所需的大模型对话接口，便于单测替换。
type LLMChatClient interface {
	Chat(ctx context.Context, messages []llm.ChatMessage) (string, error)
}

type aiSliceWorker struct {
	taskRepo         repository.TaskRepository
	liveMaterialRepo repository.LiveMaterialRepository
	videoProjectRepo repository.VideoProjectRepository
	llmClient        LLMChatClient
	logger           *zap.Logger
	concurrency      int
	pollInterval     time.Duration

	wake      chan struct{}
	startOnce sync.Once
}

// NewAISliceWorker 创建 AI 切片后台 Worker。
func NewAISliceWorker(
	taskRepo repository.TaskRepository,
	liveMaterialRepo repository.LiveMaterialRepository,
	videoProjectRepo repository.VideoProjectRepository,
	llmClient LLMChatClient,
	logger *zap.Logger,
) AISliceWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &aiSliceWorker{
		taskRepo:         taskRepo,
		liveMaterialRepo: liveMaterialRepo,
		videoProjectRepo: videoProjectRepo,
		llmClient:        llmClient,
		logger:           logger,
		concurrency:      aiSliceDefaultConcurrency,
		pollInterval:     aiSlicePollInterval,
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

// Start 启动若干并发领取循环 + 定时 poll，保证重启后 pending 任务仍会被处理。
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
		w.logger.Info("AI 切片 Worker 已启动", zap.Int("concurrency", n))
	})
}

// pollLoop 定时唤醒，避免仅依赖内存通道导致任务滞留。
func (w *aiSliceWorker) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Enqueue()
		}
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
			zap.Uint("task_id", task.ID),
		)
		if err := w.Process(ctx, task); err != nil {
			w.logger.Error("AI 切片任务执行失败",
				zap.Uint("task_id", task.ID),
				zap.Error(err),
			)
		}
	}
}

// Process 执行单条 AI 切片：读 ASR → 调 LLM → 回写已有 video_project.clips0/clips1 → 更新任务状态/进度。
func (w *aiSliceWorker) Process(ctx context.Context, task *model.Task) error {
	if task == nil {
		return fmt.Errorf("task 不能为空")
	}
	progress := int16(5)
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	ext, err := parseTaskExt(task.Ext)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("解析任务 ext 失败: %w", err))
	}
	if ext.VideoProjectID == 0 {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("任务缺少 video_project_id"))
	}

	project, err := w.videoProjectRepo.GetByID(ctx, ext.VideoProjectID)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("查询剪辑项目失败: %w", err))
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

	progress = 20
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	utterances := asr.FormatUtterancesForAPI(material.LiveASR)
	if len(utterances) == 0 {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("ASR 分句为空，无法进行 AI 切片"))
	}

	targetMs := ext.TargetDurationMs
	if targetMs <= 0 {
		targetMs = 60000
	}

	progress = 40
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	sysPrompt := strings.TrimSpace(task.SysPrompt)
	if sysPrompt == "" {
		sysPrompt = aiSliceDefaultSysPrompt
	}
	userContent, err := buildAISliceUserPrompt(task.UsrPrompt, targetMs, utterances)
	if err != nil {
		return w.fail(ctx, task.ID, progress, err)
	}

	if w.llmClient == nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("LLM 客户端未配置"))
	}

	content, err := w.llmClient.Chat(ctx, []llm.ChatMessage{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userContent},
	})
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("调用大模型失败: %w", err))
	}

	progress = 70
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	ranges, err := llm.ParseClipRanges(content)
	if err != nil {
		return w.fail(ctx, task.ID, progress, err)
	}

	clips0 := ranges
	if clips0 == nil {
		clips0 = []model.ClipRange{}
	}
	clips1 := buildClips1(utterances, ranges)
	if clips1 == nil {
		clips1 = []model.ClipWithText{}
	}

	progress = 85
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	// 回写已有剪辑项目的切片结果，不新建项目。
	project.Clips0 = clips0
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

	if err := w.taskRepo.MarkCompleted(ctx, task.ID, 100, extRaw); err != nil {
		return fmt.Errorf("标记任务完成失败: %w", err)
	}
	w.logger.Info("AI 切片任务完成",
		zap.Uint("task_id", task.ID),
		zap.Uint("video_project_id", project.ID),
		zap.Int("clips", len(ranges)),
	)
	return nil
}

func (w *aiSliceWorker) fail(ctx context.Context, taskID uint, progress int16, err error) error {
	w.logger.Error("AI 切片任务失败",
		zap.Uint("task_id", taskID),
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

// buildAISliceUserPrompt 组装发给大模型的用户侧提示（含目标时长与 ASR 分句）。
func buildAISliceUserPrompt(usrPrompt string, targetMs int64, utterances []asr.Utterance) (string, error) {
	asrJSON, err := json.Marshal(utterances)
	if err != nil {
		return "", fmt.Errorf("序列化 ASR 分句失败: %w", err)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("目标成片总时长约 %d 毫秒。\n", targetMs))
	if strings.TrimSpace(usrPrompt) != "" {
		b.WriteString("附加要求：\n")
		b.WriteString(strings.TrimSpace(usrPrompt))
		b.WriteString("\n")
	}
	b.WriteString("以下是完整 ASR 分句 JSON（时间单位为毫秒）：\n")
	b.Write(asrJSON)
	return b.String(), nil
}
