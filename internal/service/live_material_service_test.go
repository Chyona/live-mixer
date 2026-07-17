package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// mockLiveMaterialRepo 用于直播素材 service 单元测试的仓储 mock。
type mockLiveMaterialRepo struct {
	materials map[uint]*model.LiveMaterial
	nextID    uint
	createFn  func(ctx context.Context, material *model.LiveMaterial) error
	updateFn  func(ctx context.Context, material *model.LiveMaterial) error
	deleteFn func(ctx context.Context, id uint) error
	listFn    func(ctx context.Context, filter repository.LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error)
}

func (m *mockLiveMaterialRepo) Create(ctx context.Context, material *model.LiveMaterial) error {
	if m.createFn != nil {
		return m.createFn(ctx, material)
	}
	m.nextID++
	material.ID = m.nextID
	if m.materials == nil {
		m.materials = make(map[uint]*model.LiveMaterial)
	}
	// 深拷贝一份存入 map，便于断言。
	stored := *material
	m.materials[material.ID] = &stored
	return nil
}

func (m *mockLiveMaterialRepo) GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	material, ok := m.materials[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	stored := *material
	return &stored, nil
}

func (m *mockLiveMaterialRepo) UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, material)
	}
	existing, ok := m.materials[material.ID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	existing.Name = material.Name
	existing.Remark = material.Remark
	return nil
}

func (m *mockLiveMaterialRepo) List(ctx context.Context, filter repository.LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter, offset, limit)
	}
	return nil, 0, nil
}

func (m *mockLiveMaterialRepo) ClaimPendingASR(ctx context.Context) (*model.LiveMaterial, error) {
	return nil, nil
}
func (m *mockLiveMaterialRepo) RequeueStaleProcessingASR(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockLiveMaterialRepo) UpdateASRProcessing(ctx context.Context, id uint) error { return nil }
func (m *mockLiveMaterialRepo) UpdateASRProgress(ctx context.Context, id uint, progress int16) error {
	return nil
}
func (m *mockLiveMaterialRepo) UpdateASRCompleted(ctx context.Context, id uint, liveASR string, duration int64) error {
	return nil
}
func (m *mockLiveMaterialRepo) UpdateASRFailed(ctx context.Context, id uint, progress int16, errorMsg string) error {
	return nil
}
func (m *mockLiveMaterialRepo) ResetASRToPending(ctx context.Context, id uint) error {
	if m.materials == nil {
		return nil
	}
	material, ok := m.materials[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if material.ASRStatus != model.ASRStatusFailed {
		return gorm.ErrRecordNotFound
	}
	material.ASRStatus = model.ASRStatusPending
	material.ASRProgress = 0
	material.LiveASR = "{}"
	material.ASRErrorMsg = ""
	material.ASRStartedAt = nil
	return nil
}

func (m *mockLiveMaterialRepo) Delete(ctx context.Context, id uint) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	delete(m.materials, id)
	return nil
}

// TestLiveMaterialService_Create_Success 验证创建时写入默认值且创建人正确。
func TestLiveMaterialService_Create_Success(t *testing.T) {
	repo := &mockLiveMaterialRepo{}
	svc := NewLiveMaterialService(repo, nil)

	material, err := svc.Create(context.Background(), 2, "  测试素材  ", " https://example.com/live.mp4 ", "备注", "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if material.Name != "测试素材" {
		t.Errorf("Name = %q, want 测试素材", material.Name)
	}
	if material.LiveURL != "https://example.com/live.mp4" {
		t.Errorf("LiveURL = %q, want https://example.com/live.mp4", material.LiveURL)
	}
	if material.Remark != "备注" {
		t.Errorf("Remark = %q, want 备注", material.Remark)
	}
	if material.CreatedBy != 2 {
		t.Errorf("CreatedBy = %d, want 2", material.CreatedBy)
	}
	if material.LiveASR != "{}" {
		t.Errorf("LiveASR = %q, want {}", material.LiveASR)
	}
	if material.ASRStatus != model.ASRStatusPending {
		t.Errorf("ASRStatus = %q, want %q", material.ASRStatus, model.ASRStatusPending)
	}
	if material.ASRProgress != 0 {
		t.Errorf("ASRProgress = %d, want 0", material.ASRProgress)
	}
}

// TestLiveMaterialService_Create_EmptyName 验证名称为纯空格时拒绝创建。
func TestLiveMaterialService_Create_EmptyName(t *testing.T) {
	svc := NewLiveMaterialService(&mockLiveMaterialRepo{}, nil)
	_, err := svc.Create(context.Background(), 1, "   ", "https://example.com/live.mp4", "", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if err.Error() != "素材名称不能为空" {
		t.Errorf("error = %q, want 素材名称不能为空", err.Error())
	}
}

// TestLiveMaterialService_Create_EmptyLiveURL 验证直播链接为空时拒绝创建。
func TestLiveMaterialService_Create_EmptyLiveURL(t *testing.T) {
	svc := NewLiveMaterialService(&mockLiveMaterialRepo{}, nil)
	_, err := svc.Create(context.Background(), 1, "素材", "  ", "", "")
	if err == nil {
		t.Fatal("expected error for empty live_url")
	}
	if err.Error() != "直播链接不能为空" {
		t.Errorf("error = %q, want 直播链接不能为空", err.Error())
	}
}

// TestLiveMaterialService_Update_Success 验证仅更新 name、remark。
func TestLiveMaterialService_Update_Success(t *testing.T) {
	repo := &mockLiveMaterialRepo{
		materials: map[uint]*model.LiveMaterial{
			1: {
				ID:      1,
				Name:    "旧名称",
				Remark:  "旧备注",
				LiveURL: "https://example.com/old.mp4",
			},
		},
	}
	svc := NewLiveMaterialService(repo, nil)

	material, err := svc.Update(context.Background(), 1, "  新名称  ", "新备注")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if material.Name != "新名称" {
		t.Errorf("Name = %q, want 新名称", material.Name)
	}
	if material.Remark != "新备注" {
		t.Errorf("Remark = %q, want 新备注", material.Remark)
	}
	// live_url 应保持不变。
	if material.LiveURL != "https://example.com/old.mp4" {
		t.Errorf("LiveURL = %q, want unchanged old url", material.LiveURL)
	}
}

// TestLiveMaterialService_Update_NotFound 验证素材不存在时返回错误。
func TestLiveMaterialService_Update_NotFound(t *testing.T) {
	svc := NewLiveMaterialService(&mockLiveMaterialRepo{materials: map[uint]*model.LiveMaterial{}}, nil)
	_, err := svc.Update(context.Background(), 99, "名称", "备注")
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if err.Error() != "直播素材不存在" {
		t.Errorf("error = %q, want 直播素材不存在", err.Error())
	}
}

// TestLiveMaterialService_Update_EmptyName 验证更新时名称不能为空。
func TestLiveMaterialService_Update_EmptyName(t *testing.T) {
	repo := &mockLiveMaterialRepo{
		materials: map[uint]*model.LiveMaterial{
			1: {ID: 1, Name: "旧名称", LiveURL: "https://example.com/a.mp4"},
		},
	}
	svc := NewLiveMaterialService(repo, nil)

	_, err := svc.Update(context.Background(), 1, "   ", "备注")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if err.Error() != "素材名称不能为空" {
		t.Errorf("error = %q, want 素材名称不能为空", err.Error())
	}
}

// TestLiveMaterialService_Create_RepoError 验证仓储异常时向上传递。
func TestLiveMaterialService_Create_RepoError(t *testing.T) {
	repo := &mockLiveMaterialRepo{
		createFn: func(ctx context.Context, material *model.LiveMaterial) error {
			return errors.New("db down")
		},
	}
	svc := NewLiveMaterialService(repo, nil)
	_, err := svc.Create(context.Background(), 1, "素材", "https://example.com/a.mp4", "", "")
	if err == nil || err.Error() != "db down" {
		t.Errorf("error = %v, want db down", err)
	}
}

// TestLiveMaterialService_List_Pagination 验证分页 offset 计算正确并返回仓储结果。
func TestLiveMaterialService_List_Pagination(t *testing.T) {
	var gotOffset, gotLimit int
	repo := &mockLiveMaterialRepo{
		listFn: func(ctx context.Context, filter repository.LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
			gotOffset = offset
			gotLimit = limit
			return []model.LiveMaterialListItem{
				{ID: 2, Name: "素材B", ASRStatus: model.ASRStatusCompleted},
			}, 5, nil
		},
	}
	svc := NewLiveMaterialService(repo, nil)

	materials, total, err := svc.List(context.Background(), 2, 20, LiveMaterialListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if gotOffset != 20 || gotLimit != 20 {
		t.Errorf("offset/limit = %d/%d, want 20/20", gotOffset, gotLimit)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(materials) != 1 || materials[0].Name != "素材B" {
		t.Errorf("unexpected materials: %+v", materials)
	}
}

// TestLiveMaterialService_Get_Success 验证按 ID 返回完整素材（含 live_asr）。
func TestLiveMaterialService_Get_Success(t *testing.T) {
	repo := &mockLiveMaterialRepo{
		materials: map[uint]*model.LiveMaterial{
			1: {
				ID:      1,
				Name:    "素材A",
				LiveASR: `{"result":{"text":"识别内容"}}`,
			},
		},
	}
	svc := NewLiveMaterialService(repo, nil)

	material, err := svc.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if material.Name != "素材A" {
		t.Errorf("Name = %q, want 素材A", material.Name)
	}
	if material.LiveASR != `{"result":{"text":"识别内容"}}` {
		t.Errorf("LiveASR = %q, want full ASR json", material.LiveASR)
	}
}

// TestLiveMaterialService_Create_UnsupportedFormat 验证不支持的媒体格式拒绝创建。
func TestLiveMaterialService_Create_UnsupportedFormat(t *testing.T) {
	svc := NewLiveMaterialService(&mockLiveMaterialRepo{}, nil)
	_, err := svc.Create(context.Background(), 1, "素材", "https://example.com/audio.flac", "", "")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !errors.Is(err, ErrUnsupportedMediaFormat) {
		t.Errorf("error = %v, want ErrUnsupportedMediaFormat", err)
	}
}

// TestLiveMaterialService_Create_WakesASRWorker 验证创建成功后唤醒 ASR Worker。
func TestLiveMaterialService_Create_WakesASRWorker(t *testing.T) {
	enqueued := 0
	worker := &mockASRWorker{
		enqueueFn: func() { enqueued++ },
	}
	repo := &mockLiveMaterialRepo{}
	svc := NewLiveMaterialService(repo, worker)

	material, err := svc.Create(context.Background(), 1, "素材", "https://example.com/live.mp4", "", "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if material.ASRStatus != model.ASRStatusPending {
		t.Errorf("ASRStatus = %q, want pending", material.ASRStatus)
	}
	if enqueued != 1 {
		t.Errorf("enqueued = %d, want 1", enqueued)
	}
}

func TestLiveMaterialService_RetryASR_OnlyFailed(t *testing.T) {
	repo := &mockLiveMaterialRepo{
		materials: map[uint]*model.LiveMaterial{
			1: {ID: 1, ASRStatus: model.ASRStatusFailed, ASRErrorMsg: "boom"},
			2: {ID: 2, ASRStatus: model.ASRStatusCompleted},
			3: {ID: 3, ASRStatus: model.ASRStatusProcessing},
		},
		nextID: 3,
	}
	enqueued := 0
	svc := NewLiveMaterialService(repo, &mockASRWorker{enqueueFn: func() { enqueued++ }})

	got, err := svc.RetryASR(context.Background(), 1, false)
	if err != nil {
		t.Fatalf("RetryASR(failed) error = %v", err)
	}
	if got.ASRStatus != model.ASRStatusPending {
		t.Errorf("ASRStatus = %q, want pending", got.ASRStatus)
	}
	if enqueued != 1 {
		t.Errorf("enqueued = %d, want 1", enqueued)
	}

	if _, err := svc.RetryASR(context.Background(), 2, true); !errors.Is(err, ErrASRRetryOnlyFailed) {
		t.Errorf("RetryASR(completed) error = %v, want ErrASRRetryOnlyFailed", err)
	}
	if _, err := svc.RetryASR(context.Background(), 3, false); !errors.Is(err, ErrASRAlreadyProcessing) {
		t.Errorf("RetryASR(processing) error = %v, want ErrASRAlreadyProcessing", err)
	}
}

type mockASRWorker struct {
	enqueueFn func()
}

func (m *mockASRWorker) Enqueue() {
	if m.enqueueFn != nil {
		m.enqueueFn()
	}
}
func (m *mockASRWorker) Process(ctx context.Context, material *model.LiveMaterial) error {
	return nil
}
func (m *mockASRWorker) Start(ctx context.Context) {}

// TestLiveMaterialService_Get_NotFound 验证素材不存在时返回错误。
func TestLiveMaterialService_Get_NotFound(t *testing.T) {
	svc := NewLiveMaterialService(&mockLiveMaterialRepo{materials: map[uint]*model.LiveMaterial{}}, nil)
	_, err := svc.Get(context.Background(), 99)
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if err.Error() != "直播素材不存在" {
		t.Errorf("error = %q, want 直播素材不存在", err.Error())
	}
}

// TestParseCommaKeywords 验证逗号分隔关键词解析与去空白。
func TestParseCommaKeywords(t *testing.T) {
	got := parseCommaKeywords(" 游戏, 周末 ,, ")
	if len(got) != 2 || got[0] != "游戏" || got[1] != "周末" {
		t.Errorf("parseCommaKeywords() = %v", got)
	}
	if parseCommaKeywords("   ") != nil {
		t.Error("empty input should return nil")
	}
}

// TestBuildLiveMaterialListFilter 验证日期与关键词筛选条件构建。
func TestBuildLiveMaterialListFilter(t *testing.T) {
	filter, err := buildLiveMaterialListFilter(LiveMaterialListOptions{
		StartDate:     "2026-01-01",
		EndDate:       "2026-01-31",
		TitleKeyword:  "游戏,周末",
		GlobalKeyword: "发布会",
	})
	if err != nil {
		t.Fatalf("buildLiveMaterialListFilter() error = %v", err)
	}
	if filter.StartAt == nil || filter.EndAt == nil {
		t.Fatal("date range should be set")
	}
	if len(filter.TitleKeywords) != 2 || len(filter.GlobalKeywords) != 1 {
		t.Fatalf("unexpected keywords: title=%v global=%v", filter.TitleKeywords, filter.GlobalKeywords)
	}
}

// TestBuildLiveMaterialListFilter_InvalidDate 验证非法日期返回错误。
func TestBuildLiveMaterialListFilter_InvalidDate(t *testing.T) {
	_, err := buildLiveMaterialListFilter(LiveMaterialListOptions{StartDate: "2026/01/01"})
	if err == nil {
		t.Fatal("expected invalid date error")
	}
}

// TestBuildLiveMaterialListFilter_InvalidRange 验证开始日期晚于结束日期时返回错误。
func TestBuildLiveMaterialListFilter_InvalidRange(t *testing.T) {
	_, err := buildLiveMaterialListFilter(LiveMaterialListOptions{
		StartDate: "2026-02-01",
		EndDate:   "2026-01-01",
	})
	if err == nil {
		t.Fatal("expected invalid range error")
	}
}

// TestLiveMaterialService_List_PassesFilter 验证列表查询将筛选条件传递给仓储层。
func TestLiveMaterialService_List_PassesFilter(t *testing.T) {
	var gotFilter repository.LiveMaterialListFilter
	repo := &mockLiveMaterialRepo{
		listFn: func(ctx context.Context, filter repository.LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
			gotFilter = filter
			return nil, 0, nil
		},
	}
	svc := NewLiveMaterialService(repo, nil)
	_, _, err := svc.List(context.Background(), 1, 10, LiveMaterialListOptions{
		TitleKeyword: "游戏",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(gotFilter.TitleKeywords) != 1 || gotFilter.TitleKeywords[0] != "游戏" {
		t.Errorf("filter = %+v", gotFilter)
	}
}

// TestLiveMaterialService_Delete_Success 验证删除时调用仓储层。
func TestLiveMaterialService_Delete_Success(t *testing.T) {
	var deletedID uint
	repo := &mockLiveMaterialRepo{
		deleteFn: func(ctx context.Context, id uint) error {
			deletedID = id
			return nil
		},
	}
	svc := NewLiveMaterialService(repo, nil)
	if err := svc.Delete(context.Background(), 3); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deletedID != 3 {
		t.Errorf("deletedID = %d, want 3", deletedID)
	}
}

// TestLiveMaterialService_Delete_NotFound 验证素材不存在时返回错误。
func TestLiveMaterialService_Delete_NotFound(t *testing.T) {
	repo := &mockLiveMaterialRepo{
		deleteFn: func(ctx context.Context, id uint) error {
			return gorm.ErrRecordNotFound
		},
	}
	svc := NewLiveMaterialService(repo, nil)
	if err := svc.Delete(context.Background(), 99); err != ErrLiveMaterialNotFound {
		t.Errorf("Delete() error = %v, want %v", err, ErrLiveMaterialNotFound)
	}
}

// TestLiveMaterialService_DownloadASRSubtitle_Success 验证导出已完成的 ASR JSON。
func TestLiveMaterialService_DownloadASRSubtitle_Success(t *testing.T) {
	rawASR := `{"result":{"utterances":[{"text":"你好"}]}}`
	repo := &mockLiveMaterialRepo{
		materials: map[uint]*model.LiveMaterial{
			5: {
				ID:        5,
				ASRStatus: model.ASRStatusCompleted,
				LiveASR:   rawASR,
			},
		},
	}
	svc := NewLiveMaterialService(repo, nil)

	content, fileName, err := svc.DownloadASRSubtitle(context.Background(), 5)
	if err != nil {
		t.Fatalf("DownloadASRSubtitle() error = %v", err)
	}
	if string(content) != rawASR {
		t.Errorf("content = %q, want %q", content, rawASR)
	}
	if fileName != "asr_subtitle_5.json" {
		t.Errorf("fileName = %q, want asr_subtitle_5.json", fileName)
	}
}

// TestLiveMaterialService_DownloadASRSubtitle_NotReady 验证未完成 ASR 时拒绝导出。
func TestLiveMaterialService_DownloadASRSubtitle_NotReady(t *testing.T) {
	repo := &mockLiveMaterialRepo{
		materials: map[uint]*model.LiveMaterial{
			1: {ID: 1, ASRStatus: model.ASRStatusProcessing, LiveASR: `{"result":{}}`},
		},
	}
	svc := NewLiveMaterialService(repo, nil)
	_, _, err := svc.DownloadASRSubtitle(context.Background(), 1)
	if !errors.Is(err, ErrASRSubtitleNotReady) {
		t.Errorf("error = %v, want %v", err, ErrASRSubtitleNotReady)
	}
}

// TestLiveMaterialService_DownloadASRSubtitle_Empty 验证空字幕拒绝导出。
func TestLiveMaterialService_DownloadASRSubtitle_Empty(t *testing.T) {
	repo := &mockLiveMaterialRepo{
		materials: map[uint]*model.LiveMaterial{
			1: {ID: 1, ASRStatus: model.ASRStatusCompleted, LiveASR: "{}"},
		},
	}
	svc := NewLiveMaterialService(repo, nil)
	_, _, err := svc.DownloadASRSubtitle(context.Background(), 1)
	if !errors.Is(err, ErrASRSubtitleEmpty) {
		t.Errorf("error = %v, want %v", err, ErrASRSubtitleEmpty)
	}
}

// TestLiveMaterialService_DownloadASRSubtitle_NotFound 验证素材不存在。
func TestLiveMaterialService_DownloadASRSubtitle_NotFound(t *testing.T) {
	svc := NewLiveMaterialService(&mockLiveMaterialRepo{materials: map[uint]*model.LiveMaterial{}}, nil)
	_, _, err := svc.DownloadASRSubtitle(context.Background(), 99)
	if !errors.Is(err, ErrLiveMaterialNotFound) {
		t.Errorf("error = %v, want %v", err, ErrLiveMaterialNotFound)
	}
}
