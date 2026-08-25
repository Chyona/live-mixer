package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

var asrIndexLineRE = regexp.MustCompile(`\[(\d+)\]`)

// sampleLiveASRJSON 含单句 utterance，供后处理与完成态测试使用。
func sampleLiveASRJSON(durationMs int64, text string) json.RawMessage {
	payload := fmt.Sprintf(
		`{"audio_info":{"duration":%d},"result":{"utterances":[{"additions":{"speaker":"1"},"start_time":0,"end_time":%d,"text":%q,"words":[{"start_time":0,"end_time":%d,"text":%q}]}]}}`,
		durationMs, durationMs, text, durationMs, text,
	)
	return json.RawMessage(payload)
}

type workerMockLLM struct {
	chatFn func(ctx context.Context, messages []llm.ChatMessage) (string, error)
}

func (m *workerMockLLM) Chat(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	if m != nil && m.chatFn != nil {
		return m.chatFn(ctx, messages)
	}
	sys, user := "", ""
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			sys = msg.Content
		case "user":
			user = msg.Content
		}
	}
	if strings.Contains(sys, "主题提炼") {
		return `{"items":[{"title":"开场","start_index":0,"end_index":0}]}`, nil
	}
	maxIdx := 0
	for _, m := range asrIndexLineRE.FindAllStringSubmatch(user, -1) {
		idx, _ := strconv.Atoi(m[1])
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	return fmt.Sprintf(`{"items":[{"start_index":0,"end_index":%d}]}`, maxIdx), nil
}

func (m *workerMockLLM) ChatStructured(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	return m.Chat(ctx, messages)
}

func (m *workerMockLLM) ChatThinking(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	return m.Chat(ctx, messages)
}

func defaultWorkerLLM() LLMChatClient {
	return &workerMockLLM{}
}

type workerMockRepo struct {
	mu        sync.Mutex
	materials map[uint]*model.LiveMaterial

	requeueFn   func(ctx context.Context, olderThan time.Duration) (int64, error)
	claimCalls  int32
	requeueCalls int32
}

func (m *workerMockRepo) Create(ctx context.Context, material *model.LiveMaterial) error { return nil }
func (m *workerMockRepo) GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	material, ok := m.materials[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	stored := *material
	return &stored, nil
}
func (m *workerMockRepo) GetByName(ctx context.Context, name string) (*model.LiveMaterial, error) {
	return nil, gorm.ErrRecordNotFound
}
func (m *workerMockRepo) GetByLiveURL(ctx context.Context, liveURL string) (*model.LiveMaterial, error) {
	return nil, gorm.ErrRecordNotFound
}
func (m *workerMockRepo) UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error {
	return nil
}
func (m *workerMockRepo) List(ctx context.Context, filter repository.LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
	return nil, 0, nil
}

func (m *workerMockRepo) Delete(ctx context.Context, id uint) error { return nil }

func (m *workerMockRepo) ClaimPendingASR(ctx context.Context) (*model.LiveMaterial, error) {
	atomic.AddInt32(&m.claimCalls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := uint(1); id <= 1000; id++ {
		material, ok := m.materials[id]
		if !ok || material.ASRStatus != model.ASRStatusPending {
			continue
		}
		material.ASRStatus = model.ASRStatusProcessing
		material.ASRProgress = 5
		material.ASRErrorMsg = ""
		material.ASRVersion++
		now := time.Now()
		material.ASRStartedAt = &now
		stored := *material
		return &stored, nil
	}
	return nil, nil
}

func (m *workerMockRepo) RequeueStaleProcessingASR(ctx context.Context, olderThan time.Duration) (int64, error) {
	atomic.AddInt32(&m.requeueCalls, 1)
	if m.requeueFn != nil {
		return m.requeueFn(ctx, olderThan)
	}
	return 0, nil
}

func (m *workerMockRepo) UpdateASRProcessing(ctx context.Context, id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	material := m.materials[id]
	if material == nil {
		return gorm.ErrRecordNotFound
	}
	material.ASRStatus = model.ASRStatusProcessing
	material.ASRProgress = 5
	material.ASRErrorMsg = ""
	return nil
}

func (m *workerMockRepo) UpdateASRProgress(ctx context.Context, id uint, asrVersion int64, progress int16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	material := m.materials[id]
	if material == nil {
		return gorm.ErrRecordNotFound
	}
	if material.ASRStatus != model.ASRStatusProcessing || material.ASRVersion != asrVersion {
		return nil
	}
	material.ASRProgress = progress
	return nil
}

func (m *workerMockRepo) UpdateASRCompleted(ctx context.Context, id uint, asrVersion int64, liveASR string, duration int64, width, height int, summaries []model.ASRSummarySegment, paragraphs []model.ASRParagraph) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	material := m.materials[id]
	if material == nil {
		return gorm.ErrRecordNotFound
	}
	if material.ASRStatus != model.ASRStatusProcessing || material.ASRVersion != asrVersion {
		return nil
	}
	material.ASRStatus = model.ASRStatusCompleted
	material.ASRProgress = 100
	material.LiveASR = liveASR
	material.Duration = duration
	material.Width = width
	material.Height = height
	material.ASRSummaries = summaries
	material.ASRParagraphs = paragraphs
	return nil
}

func (m *workerMockRepo) UpdateASRFailed(ctx context.Context, id uint, asrVersion int64, progress int16, errorMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	material := m.materials[id]
	if material == nil {
		return gorm.ErrRecordNotFound
	}
	if material.ASRStatus != model.ASRStatusProcessing || material.ASRVersion != asrVersion {
		return nil
	}
	material.ASRStatus = model.ASRStatusFailed
	material.ASRProgress = progress
	material.ASRErrorMsg = errorMsg
	return nil
}

func (m *workerMockRepo) ResetASRToPending(ctx context.Context, id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	material := m.materials[id]
	if material == nil {
		return gorm.ErrRecordNotFound
	}
	material.ASRStatus = model.ASRStatusPending
	material.ASRProgress = 0
	material.LiveASR = "{}"
	material.ASRSummaries = nil
	material.ASRParagraphs = nil
	material.ASRErrorMsg = ""
	material.ASRStartedAt = nil
	return nil
}

func (m *workerMockRepo) countByStatus(status string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, material := range m.materials {
		if material.ASRStatus == status {
			n++
		}
	}
	return n
}

type workerMockASR struct {
	transcribeFn func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error)
}

type mockASRAudioPreparer struct {
	prepareFn func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error)
}

func (m *mockASRAudioPreparer) Prepare(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
	return m.prepareFn(ctx, materialID, sourceURL, onProgress)
}

func (m *workerMockASR) Transcribe(ctx context.Context, audioURL string) (json.RawMessage, error) {
	return m.TranscribeWithProgress(ctx, audioURL, nil)
}

func (m *workerMockASR) TranscribeWithProgress(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
	if m.transcribeFn != nil {
		return m.transcribeFn(ctx, audioURL, onProgress)
	}
	return nil, nil
}

func TestLiveMaterialASRWorker_DefaultConcurrencyIsSix(t *testing.T) {
	w := NewLiveMaterialASRWorker(&workerMockRepo{materials: map[uint]*model.LiveMaterial{}}, &workerMockASR{}, nil, nil, nil, 0, 0, webroot.Config{})
	impl, ok := w.(*liveMaterialASRWorker)
	if !ok {
		t.Fatal("unexpected worker type")
	}
	if impl.concurrency != 6 {
		t.Fatalf("concurrency = %d, want 6", impl.concurrency)
	}
	if liveMaterialASRDefaultConcurrency != 6 {
		t.Fatalf("liveMaterialASRDefaultConcurrency = %d, want 6", liveMaterialASRDefaultConcurrency)
	}
}

func TestLiveMaterialASRWorker_UsesConfiguredConcurrency(t *testing.T) {
	w := NewLiveMaterialASRWorker(&workerMockRepo{materials: map[uint]*model.LiveMaterial{}}, &workerMockASR{}, nil, nil, nil, 3, 0, webroot.Config{}).(*liveMaterialASRWorker)
	if w.concurrency != 3 {
		t.Fatalf("concurrency = %d, want 3", w.concurrency)
	}
}

// TestLiveMaterialASRWorker_DefaultAndConfiguredStaleTimeout ?????????????????
func TestLiveMaterialASRWorker_DefaultAndConfiguredStaleTimeout(t *testing.T) {
	defaultW := NewLiveMaterialASRWorker(&workerMockRepo{materials: map[uint]*model.LiveMaterial{}}, &workerMockASR{}, nil, nil, nil, 0, 0, webroot.Config{}).(*liveMaterialASRWorker)
	if defaultW.staleTimeout != liveMaterialASRStaleTimeout {
		t.Fatalf("default staleTimeout = %v, want %v", defaultW.staleTimeout, liveMaterialASRStaleTimeout)
	}
	custom := NewLiveMaterialASRWorker(&workerMockRepo{materials: map[uint]*model.LiveMaterial{}}, &workerMockASR{}, nil, nil, nil, 0, 15*time.Minute, webroot.Config{}).(*liveMaterialASRWorker)
	if custom.staleTimeout != 15*time.Minute {
		t.Fatalf("custom staleTimeout = %v, want 15m", custom.staleTimeout)
	}
}

func TestLiveMaterialASRWorker_Process_Success(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			1: {
				ID:        1,
				LiveURL:   "https://example.com/live.mp4",
				ASRStatus: model.ASRStatusProcessing,
			},
		},
	}
	var asrAudioURL string
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			asrAudioURL = audioURL
			if onProgress != nil {
				onProgress(50)
			}
			// 10 分钟：summary 段可通过 [5,60] 分钟过滤。
			return sampleLiveASRJSON(10*60*1000, "hello"), nil
		},
	}
	preparer := &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			if sourceURL != "https://example.com/live.mp4" {
				t.Errorf("sourceURL = %q, want live mp4 url", sourceURL)
			}
			if onProgress != nil {
				onProgress(45)
			}
			return ASRAudioPrepareResult{AudioURL: "https://bucket.example.com/video_editing/temp/asr/1/test.mp3", Width: 1280, Height: 720, Cleanup: func() {}}, nil
		},
	}
	worker := NewLiveMaterialASRWorker(repo, asrSvc, preparer, defaultWorkerLLM(), nil, 0, 0, webroot.Config{})

	if err := worker.Process(context.Background(), repo.materials[1]); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	material := repo.materials[1]
	if material.ASRStatus != model.ASRStatusCompleted {
		t.Errorf("ASRStatus = %q, want completed", material.ASRStatus)
	}
	if material.ASRProgress != 100 {
		t.Errorf("ASRProgress = %d, want 100", material.ASRProgress)
	}
	if material.Duration != 10*60*1000 {
		t.Errorf("Duration = %d, want %d", material.Duration, 10*60*1000)
	}
	if material.LiveASR == "" {
		t.Error("LiveASR should not be empty")
	}
	if asrAudioURL != "https://bucket.example.com/video_editing/temp/asr/1/test.mp3" {
		t.Errorf("ASR audio URL = %q, want uploaded mp3 url", asrAudioURL)
	}
	if material.Width != 1280 || material.Height != 720 {
		t.Errorf("Width/Height = %d/%d, want 1280x720", material.Width, material.Height)
	}
	if len(material.ASRSummaries) == 0 {
		t.Error("ASRSummaries should not be empty after LLM postprocess")
	}
	if len(material.ASRParagraphs) == 0 {
		t.Error("ASRParagraphs should not be empty after LLM postprocess")
	}
	if material.ASRParagraphs[0].Text != "hello" {
		t.Errorf("ASRParagraphs[0].Text = %q, want hello", material.ASRParagraphs[0].Text)
	}
}

// TestLiveMaterialASRWorker_Process_StaleVersionIgnored 验证超时回收后旧 Worker 写回被忽略，asr_started_at 不被清空。
func TestLiveMaterialASRWorker_Process_StaleVersionIgnored(t *testing.T) {
	started := time.Now().Add(-time.Minute)
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			10: {
				ID:           10,
				LiveURL:      "https://example.com/stale.mp4",
				ASRStatus:    model.ASRStatusProcessing,
				ASRVersion:   2,
				ASRStartedAt: &started,
			},
		},
	}
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			return sampleLiveASRJSON(500, "stale"), nil
		},
	}
	worker := NewLiveMaterialASRWorker(repo, asrSvc, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			return ASRAudioPrepareResult{AudioURL: "https://bucket.example.com/temp/stale.mp3", Cleanup: func() {}}, nil
		},
	}, defaultWorkerLLM(), nil, 0, 0, webroot.Config{})

	// 模拟 pollLoop 超时回收：version 递增、status 改回 pending，保留 started_at。
	repo.mu.Lock()
	repo.materials[10].ASRStatus = model.ASRStatusPending
	repo.materials[10].ASRProgress = 0
	repo.materials[10].ASRVersion = 3
	repo.materials[10].ASRErrorMsg = "ASR 处理超时，已自动重新排队"
	repo.mu.Unlock()

	// 旧 Worker 仍持有 version=2 继续 Process，写回应被忽略。
	oldSnapshot := &model.LiveMaterial{
		ID:         10,
		LiveURL:    "https://example.com/stale.mp4",
		ASRStatus:  model.ASRStatusProcessing,
		ASRVersion: 2,
	}
	if err := worker.Process(context.Background(), oldSnapshot); err != nil {
		t.Fatalf("Process(stale) error = %v", err)
	}

	got := repo.materials[10]
	if got.ASRStatus != model.ASRStatusPending {
		t.Errorf("ASRStatus = %q, want pending", got.ASRStatus)
	}
	if got.ASRVersion != 3 {
		t.Errorf("ASRVersion = %d, want 3", got.ASRVersion)
	}
	if got.ASRStartedAt == nil || !got.ASRStartedAt.Equal(started) {
		t.Errorf("asr_started_at = %v, want preserved %v", got.ASRStartedAt, started)
	}
	if got.LiveASR != "" && got.LiveASR != "{}" {
		t.Errorf("LiveASR = %q, want unchanged by stale complete", got.LiveASR)
	}
}

func TestLiveMaterialASRWorker_Process_Failed(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			2: {
				ID:        2,
				LiveURL:   "https://example.com/live.mp3",
				ASRStatus: model.ASRStatusProcessing,
			},
		},
	}
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			if onProgress != nil {
				onProgress(20)
			}
			return nil, errors.New("ASR ????")
		},
	}
	worker := NewLiveMaterialASRWorker(repo, asrSvc, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			return ASRAudioPrepareResult{AudioURL: "https://bucket.example.com/temp/asr.mp3", Cleanup: func() {}}, nil
		},
	}, nil, nil, 0, 0, webroot.Config{})

	if err := worker.Process(context.Background(), repo.materials[2]); err == nil {
		t.Fatal("Process() error = nil, want error")
	}

	material := repo.materials[2]
	if material.ASRStatus != model.ASRStatusFailed {
		t.Errorf("ASRStatus = %q, want failed", material.ASRStatus)
	}
	if material.ASRErrorMsg == "" {
		t.Error("ASRErrorMsg should not be empty")
	}
}

func TestLiveMaterialASRWorker_Process_SkipNonProcessing(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			3: {ID: 3, ASRStatus: model.ASRStatusCompleted},
		},
	}
	called := false
	prepareCalled := false
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			called = true
			return nil, nil
		},
	}
	worker := NewLiveMaterialASRWorker(repo, asrSvc, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			prepareCalled = true
			return ASRAudioPrepareResult{AudioURL: "https://bucket.example.com/temp/asr.mp3", Cleanup: func() {}}, nil
		},
	}, nil, nil, 0, 0, webroot.Config{})

	if err := worker.Process(context.Background(), repo.materials[3]); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if called || prepareCalled {
		t.Error("ASR and preparer should not be called for non-processing material")
	}
}

func TestLiveMaterialASRWorker_Process_PrepareFailed(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			4: {ID: 4, LiveURL: "https://example.com/live.mp4", ASRStatus: model.ASRStatusProcessing},
		},
	}
	worker := NewLiveMaterialASRWorker(repo, &workerMockASR{}, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			return ASRAudioPrepareResult{}, errors.New("download failed")
		},
	}, nil, nil, 0, 0, webroot.Config{})

	if err := worker.Process(context.Background(), repo.materials[4]); err == nil {
		t.Fatal("Process() error = nil, want prepare error")
	}
	if repo.materials[4].ASRStatus != model.ASRStatusFailed {
		t.Errorf("ASRStatus = %q, want failed", repo.materials[4].ASRStatus)
	}
}

func TestLiveMaterialASRWorker_Process_FallbackWithoutPreparer(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			5: {ID: 5, LiveURL: "https://example.com/direct.mp3", ASRStatus: model.ASRStatusProcessing},
		},
	}
	var asrURL string
	worker := NewLiveMaterialASRWorker(repo, &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			asrURL = audioURL
			return sampleLiveASRJSON(1, "hi"), nil
		},
	}, nil, defaultWorkerLLM(), nil, 0, 0, webroot.Config{})

	if err := worker.Process(context.Background(), repo.materials[5]); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if asrURL != "https://example.com/direct.mp3" {
		t.Errorf("ASR URL = %q, want original live url", asrURL)
	}
}

func TestLiveMaterialASRWorker_Enqueue_NonBlockingAndCoalesced(t *testing.T) {
	w := NewLiveMaterialASRWorker(&workerMockRepo{materials: map[uint]*model.LiveMaterial{}}, &workerMockASR{}, nil, nil, nil, 0, 0, webroot.Config{}).(*liveMaterialASRWorker)
	w.Enqueue()
	w.Enqueue()
	w.Enqueue()
	if got := len(w.wake); got != 1 {
		t.Fatalf("wake queue len = %d, want 1", got)
	}
}

func TestLiveMaterialASRWorker_ClaimPendingASR_ConcurrentUnique(t *testing.T) {
	materials := make(map[uint]*model.LiveMaterial, 20)
	for i := uint(1); i <= 20; i++ {
		materials[i] = &model.LiveMaterial{
			ID: i, LiveURL: "https://example.com/a.mp4", ASRStatus: model.ASRStatusPending,
		}
	}
	repo := &workerMockRepo{materials: materials}

	var (
		mu       sync.Mutex
		claimed  = make(map[uint]int)
		wg       sync.WaitGroup
		workers  = 12
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for {
				m, err := repo.ClaimPendingASR(context.Background())
				if err != nil {
					t.Errorf("ClaimPendingASR() error = %v", err)
					return
				}
				if m == nil {
					return
				}
				mu.Lock()
				claimed[m.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(claimed) != 20 {
		t.Fatalf("claimed unique = %d, want 20", len(claimed))
	}
	for id, n := range claimed {
		if n != 1 {
			t.Fatalf("material %d claimed %d times, want 1", id, n)
		}
	}
	if repo.countByStatus(model.ASRStatusPending) != 0 {
		t.Fatalf("pending left = %d, want 0", repo.countByStatus(model.ASRStatusPending))
	}
	if repo.countByStatus(model.ASRStatusProcessing) != 20 {
		t.Fatalf("processing = %d, want 20", repo.countByStatus(model.ASRStatusProcessing))
	}
}

func TestLiveMaterialASRWorker_Start_ProcessesAtMostSixConcurrently(t *testing.T) {
	const total = 10
	materials := make(map[uint]*model.LiveMaterial, total)
	for i := uint(1); i <= total; i++ {
		materials[i] = &model.LiveMaterial{
			ID: i, LiveURL: "https://example.com/live.mp4", ASRStatus: model.ASRStatusPending,
		}
	}
	repo := &workerMockRepo{materials: materials}

	var (
		inFlight    int32
		maxInFlight int32
		processed   int32
	)

	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			cur := atomic.AddInt32(&inFlight, 1)
			for {
				prev := atomic.LoadInt32(&maxInFlight)
				if cur <= prev || atomic.CompareAndSwapInt32(&maxInFlight, prev, cur) {
					break
				}
			}
			// ??????? poll ???? worker????? 6 ????
			time.Sleep(300 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			atomic.AddInt32(&processed, 1)
			return sampleLiveASRJSON(100, "ok"), nil
		},
	}
	preparer := &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			return ASRAudioPrepareResult{AudioURL: "https://bucket.example.com/temp/asr.mp3", Cleanup: func() {}}, nil
		},
	}

	worker := NewLiveMaterialASRWorker(repo, asrSvc, preparer, defaultWorkerLLM(), nil, 0, 0, webroot.Config{}).(*liveMaterialASRWorker)
	// ? poll?wake ??? 1??? poll ?????? worker ???? 6 ????
	worker.pollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	worker.Enqueue()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&processed) == total && repo.countByStatus(model.ASRStatusCompleted) == total {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if got := atomic.LoadInt32(&processed); got != total {
		t.Fatalf("processed = %d, want %d", got, total)
	}
	if repo.countByStatus(model.ASRStatusCompleted) != total {
		t.Fatalf("completed = %d, want %d", repo.countByStatus(model.ASRStatusCompleted), total)
	}
	max := atomic.LoadInt32(&maxInFlight)
	if max > 6 {
		t.Fatalf("max concurrent ASR = %d, want <= 6", max)
	}
	if max < 6 {
		t.Fatalf("max concurrent ASR = %d, want 6 (with %d pending tasks)", max, total)
	}
}

func TestLiveMaterialASRWorker_Start_DrainsPendingAfterWake(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			1: {ID: 1, LiveURL: "https://example.com/a.mp4", ASRStatus: model.ASRStatusPending},
			2: {ID: 2, LiveURL: "https://example.com/b.mp4", ASRStatus: model.ASRStatusPending},
			3: {ID: 3, LiveURL: "https://example.com/c.mp4", ASRStatus: model.ASRStatusPending},
		},
	}
	worker := NewLiveMaterialASRWorker(repo, &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			return sampleLiveASRJSON(10, "ok"), nil
		},
	}, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			return ASRAudioPrepareResult{AudioURL: "https://bucket.example.com/temp/asr.mp3", Cleanup: func() {}}, nil
		},
	}, defaultWorkerLLM(), nil, 0, 0, webroot.Config{}).(*liveMaterialASRWorker)
	worker.pollInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	worker.Enqueue()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if repo.countByStatus(model.ASRStatusCompleted) == 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("completed = %d, want 3", repo.countByStatus(model.ASRStatusCompleted))
}

func TestLiveMaterialASRWorker_PollLoop_RequeuesStale(t *testing.T) {
	var gotOlderThan time.Duration
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{},
		requeueFn: func(ctx context.Context, olderThan time.Duration) (int64, error) {
			gotOlderThan = olderThan
			return 2, nil
		},
	}
	worker := NewLiveMaterialASRWorker(repo, &workerMockASR{}, nil, nil, nil, 0, 0, webroot.Config{}).(*liveMaterialASRWorker)
	worker.pollInterval = 20 * time.Millisecond
	worker.staleTimeout = 45 * time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&repo.requeueCalls) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&repo.requeueCalls) == 0 {
		t.Fatal("expected RequeueStaleProcessingASR to be called by pollLoop")
	}
	if gotOlderThan != 45*time.Minute {
		t.Fatalf("olderThan = %v, want 45m", gotOlderThan)
	}
}

// TestLiveMaterialASRWorker_Start_RequeuesStaleImmediately ?? Start ??????????
func TestLiveMaterialASRWorker_Start_RequeuesStaleImmediately(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{},
		requeueFn: func(ctx context.Context, olderThan time.Duration) (int64, error) {
			return 1, nil
		},
	}
	worker := NewLiveMaterialASRWorker(repo, &workerMockASR{}, nil, nil, nil, 0, 0, webroot.Config{}).(*liveMaterialASRWorker)
	// ?? poll??????????? Start ????????? pollLoop?
	worker.pollInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&repo.requeueCalls) >= 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected Start to call RequeueStaleProcessingASR immediately")
}
