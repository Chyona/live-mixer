package service

import (
	"context"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// mockVideoProjectRepo 用于剪辑项目 service 单元测试的仓储 mock。
type mockVideoProjectRepo struct {
	projects map[uint]*model.VideoProject
	nextID   uint
	createFn func(ctx context.Context, project *model.VideoProject) error
	updateFn func(ctx context.Context, project *model.VideoProject) error
	deleteFn func(ctx context.Context, id uint) error
	listFn   func(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProjectListItem, int64, error)
}

func (m *mockVideoProjectRepo) Create(ctx context.Context, project *model.VideoProject) error {
	if m.createFn != nil {
		return m.createFn(ctx, project)
	}
	m.nextID++
	project.ID = m.nextID
	if m.projects == nil {
		m.projects = make(map[uint]*model.VideoProject)
	}
	stored := *project
	m.projects[project.ID] = &stored
	return nil
}

func (m *mockVideoProjectRepo) GetByID(ctx context.Context, id uint) (*model.VideoProject, error) {
	project, ok := m.projects[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	stored := *project
	return &stored, nil
}

func (m *mockVideoProjectRepo) Update(ctx context.Context, project *model.VideoProject) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, project)
	}
	existing, ok := m.projects[project.ID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	*existing = *project
	return nil
}

func (m *mockVideoProjectRepo) Delete(ctx context.Context, id uint) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	delete(m.projects, id)
	return nil
}

func (m *mockVideoProjectRepo) List(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProjectListItem, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter, offset, limit)
	}
	return nil, 0, nil
}

// mockLiveMaterialRepoForProject 仅用于剪辑项目创建时校验 live_id。
type mockLiveMaterialRepoForProject struct {
	materials map[uint]*model.LiveMaterial
}

func (m *mockLiveMaterialRepoForProject) Create(ctx context.Context, material *model.LiveMaterial) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	material, ok := m.materials[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	stored := *material
	return &stored, nil
}
func (m *mockLiveMaterialRepoForProject) GetByName(ctx context.Context, name string) (*model.LiveMaterial, error) {
	return nil, gorm.ErrRecordNotFound
}
func (m *mockLiveMaterialRepoForProject) GetByLiveURL(ctx context.Context, liveURL string) (*model.LiveMaterial, error) {
	return nil, gorm.ErrRecordNotFound
}
func (m *mockLiveMaterialRepoForProject) UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) ClaimPendingASR(ctx context.Context) (*model.LiveMaterial, error) {
	return nil, nil
}
func (m *mockLiveMaterialRepoForProject) RequeueStaleProcessingASR(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockLiveMaterialRepoForProject) UpdateASRProcessing(ctx context.Context, id uint) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) UpdateASRProgress(ctx context.Context, id uint, asrVersion int64, progress int16) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) UpdateASRCompleted(ctx context.Context, id uint, asrVersion int64, liveASR string, duration int64, width, height int, summaries []model.ASRSummarySegment, paragraphs []model.ASRParagraph) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) UpdateASRFailed(ctx context.Context, id uint, asrVersion int64, progress int16, errorMsg string) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) ResetASRToPending(ctx context.Context, id uint) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) List(ctx context.Context, filter repository.LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
	return nil, 0, nil
}
func (m *mockLiveMaterialRepoForProject) Delete(ctx context.Context, id uint) error { return nil }

// TestVideoProjectService_Create_Success 验证创建时写入默认值。
func TestVideoProjectService_Create_Success(t *testing.T) {
	liveRepo := &mockLiveMaterialRepoForProject{
		materials: map[uint]*model.LiveMaterial{1: {ID: 1, Name: "素材"}},
	}
	projectRepo := &mockVideoProjectRepo{}
	svc := NewVideoProjectService(projectRepo, liveRepo)

	project, err := svc.Create(context.Background(), 2, CreateVideoProjectInput{
		Name: "  项目  ", Remark: "备注", LiveID: 1, Width: 1080, Height: 1920,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if project.Name != "项目" {
		t.Errorf("Name = %q, want 项目", project.Name)
	}
	if len(project.Clips0) != 0 || len(project.Clips1) != 0 {
		t.Errorf("clips defaults = %#v/%#v, want empty", project.Clips0, project.Clips1)
	}
	if project.PromptID != model.DefaultVideoProjectPromptID {
		t.Errorf("PromptID = %d, want %d", project.PromptID, model.DefaultVideoProjectPromptID)
	}
	if project.CreatedBy != 2 || project.LiveID != 1 {
		t.Errorf("unexpected project: %+v", project)
	}
	if project.Width != 1080 || project.Height != 1920 {
		t.Errorf("Width/Height = %d/%d, want 1080/1920", project.Width, project.Height)
	}
	if project.ProjectSource != "" {
		t.Errorf("ProjectSource = %q, want empty", project.ProjectSource)
	}
	if project.EnableCaptions != model.EnableCaptionsOn {
		t.Errorf("EnableCaptions = %d, want %d", project.EnableCaptions, model.EnableCaptionsOn)
	}
}

// TestVideoProjectService_Create_EnableCaptionsOff 验证显式关闭字幕。
func TestVideoProjectService_Create_EnableCaptionsOff(t *testing.T) {
	liveRepo := &mockLiveMaterialRepoForProject{
		materials: map[uint]*model.LiveMaterial{1: {ID: 1, Name: "素材"}},
	}
	svc := NewVideoProjectService(&mockVideoProjectRepo{}, liveRepo)
	off := model.EnableCaptionsOff
	project, err := svc.Create(context.Background(), 1, CreateVideoProjectInput{
		Name: "关字幕", LiveID: 1, Width: 1080, Height: 1920, EnableCaptions: &off,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if project.EnableCaptions != model.EnableCaptionsOff {
		t.Errorf("EnableCaptions = %d, want 0", project.EnableCaptions)
	}
}

// TestVideoProjectService_Create_InvalidEnableCaptions 验证非法开关被拒绝。
func TestVideoProjectService_Create_InvalidEnableCaptions(t *testing.T) {
	liveRepo := &mockLiveMaterialRepoForProject{
		materials: map[uint]*model.LiveMaterial{1: {ID: 1, Name: "素材"}},
	}
	svc := NewVideoProjectService(&mockVideoProjectRepo{}, liveRepo)
	bad := 2
	_, err := svc.Create(context.Background(), 1, CreateVideoProjectInput{
		Name: "坏开关", LiveID: 1, EnableCaptions: &bad,
	})
	if err == nil {
		t.Fatal("expected error for invalid enable_captions")
	}
}

// TestVideoProjectService_Create_WithTypedClips 验证按结构化 clips 创建并落库。
func TestVideoProjectService_Create_WithTypedClips(t *testing.T) {
	liveRepo := &mockLiveMaterialRepoForProject{
		materials: map[uint]*model.LiveMaterial{1: {ID: 1, Name: "素材"}},
	}
	projectRepo := &mockVideoProjectRepo{}
	svc := NewVideoProjectService(projectRepo, liveRepo)

	project, err := svc.Create(context.Background(), 1, CreateVideoProjectInput{
		Name:   "项目",
		LiveID: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 1000}},
		Clips1: []model.ClipWithText{{Text: "我是中国人", StartTime: 0, EndTime: 1000}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(project.Clips0) != 1 || project.Clips0[0].EndTime != 1000 {
		t.Errorf("Clips0 = %#v", project.Clips0)
	}
	if len(project.Clips1) != 1 || project.Clips1[0].Text != "我是中国人" {
		t.Errorf("Clips1 = %#v", project.Clips1)
	}
	// 素材分辨率未知时默认竖屏档。
	if project.Width != 1080 || project.Height != 1920 {
		t.Errorf("Width/Height = %d/%d, want 1080/1920 default", project.Width, project.Height)
	}
}

func TestResolveProjectCanvasSize(t *testing.T) {
	t.Run("explicit landscape", func(t *testing.T) {
		w, h, err := resolveProjectCanvasSize(1920, 1080, 0, 0)
		if err != nil || w != 1920 || h != 1080 {
			t.Fatalf("got %dx%d err=%v", w, h, err)
		}
	})
	t.Run("explicit portrait", func(t *testing.T) {
		w, h, err := resolveProjectCanvasSize(1080, 1920, 0, 0)
		if err != nil || w != 1080 || h != 1920 {
			t.Fatalf("got %dx%d err=%v", w, h, err)
		}
	})
	t.Run("explicit invalid", func(t *testing.T) {
		_, _, err := resolveProjectCanvasSize(1280, 720, 0, 0)
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("only one side", func(t *testing.T) {
		_, _, err := resolveProjectCanvasSize(1920, 0, 1280, 720)
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("infer landscape from material", func(t *testing.T) {
		w, h, err := resolveProjectCanvasSize(0, 0, 1280, 720)
		if err != nil || w != 1920 || h != 1080 {
			t.Fatalf("got %dx%d err=%v, want 1920x1080", w, h, err)
		}
	})
	t.Run("infer portrait from material", func(t *testing.T) {
		w, h, err := resolveProjectCanvasSize(0, 0, 720, 1280)
		if err != nil || w != 1080 || h != 1920 {
			t.Fatalf("got %dx%d err=%v, want 1080x1920", w, h, err)
		}
	})
	t.Run("material unknown defaults portrait", func(t *testing.T) {
		w, h, err := resolveProjectCanvasSize(0, 0, 0, 0)
		if err != nil || w != 1080 || h != 1920 {
			t.Fatalf("got %dx%d err=%v, want 1080x1920", w, h, err)
		}
	})
	t.Run("near square prefers portrait", func(t *testing.T) {
		// r=1：距 9/16 更近，应选竖屏。
		w, h, err := resolveProjectCanvasSize(0, 0, 1000, 1000)
		if err != nil || w != 1080 || h != 1920 {
			t.Fatalf("got %dx%d err=%v, want 1080x1920", w, h, err)
		}
	})
}

func TestVideoProjectService_Create_InferCanvasFromMaterial(t *testing.T) {
	liveRepo := &mockLiveMaterialRepoForProject{
		materials: map[uint]*model.LiveMaterial{
			1: {ID: 1, Name: "横屏", Width: 1280, Height: 720},
			2: {ID: 2, Name: "竖屏", Width: 720, Height: 1280},
		},
	}
	svc := NewVideoProjectService(&mockVideoProjectRepo{}, liveRepo)

	landscape, err := svc.Create(context.Background(), 1, CreateVideoProjectInput{Name: "横", LiveID: 1})
	if err != nil {
		t.Fatalf("landscape Create: %v", err)
	}
	if landscape.Width != 1920 || landscape.Height != 1080 {
		t.Errorf("landscape = %dx%d, want 1920x1080", landscape.Width, landscape.Height)
	}

	portrait, err := svc.Create(context.Background(), 1, CreateVideoProjectInput{Name: "竖", LiveID: 2})
	if err != nil {
		t.Fatalf("portrait Create: %v", err)
	}
	if portrait.Width != 1080 || portrait.Height != 1920 {
		t.Errorf("portrait = %dx%d, want 1080x1920", portrait.Width, portrait.Height)
	}
}

func TestVideoProjectService_Create_InvalidCanvasPair(t *testing.T) {
	liveRepo := &mockLiveMaterialRepoForProject{
		materials: map[uint]*model.LiveMaterial{1: {ID: 1}},
	}
	svc := NewVideoProjectService(&mockVideoProjectRepo{}, liveRepo)
	_, err := svc.Create(context.Background(), 1, CreateVideoProjectInput{
		Name: "项目", LiveID: 1, Width: 1280, Height: 720,
	})
	if err == nil {
		t.Fatal("expected invalid canvas error")
	}
}

func TestVideoProjectService_Update_CanvasPairValidation(t *testing.T) {
	projectRepo := &mockVideoProjectRepo{
		projects: map[uint]*model.VideoProject{
			1: {ID: 1, Name: "项目", LiveID: 1, Width: 1080, Height: 1920, CreatedBy: 1},
		},
	}
	svc := NewVideoProjectService(projectRepo, &mockLiveMaterialRepoForProject{})

	w, h := 1920, 1080
	updated, err := svc.Update(context.Background(), 1, VideoProjectUpdateInput{Width: &w, Height: &h})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Width != 1920 || updated.Height != 1080 {
		t.Errorf("got %dx%d", updated.Width, updated.Height)
	}

	onlyW := 1920
	if _, err := svc.Update(context.Background(), 1, VideoProjectUpdateInput{Width: &onlyW}); err == nil {
		t.Fatal("expected pair required error")
	}
	badW, badH := 800, 600
	if _, err := svc.Update(context.Background(), 1, VideoProjectUpdateInput{Width: &badW, Height: &badH}); err == nil {
		t.Fatal("expected invalid pair error")
	}
}

// TestVideoProjectService_Create_InvalidClips 验证非法时间段被拒绝。
func TestVideoProjectService_Create_InvalidClips(t *testing.T) {
	liveRepo := &mockLiveMaterialRepoForProject{
		materials: map[uint]*model.LiveMaterial{1: {ID: 1}},
	}
	svc := NewVideoProjectService(&mockVideoProjectRepo{}, liveRepo)
	_, err := svc.Create(context.Background(), 1, CreateVideoProjectInput{
		Name: "项目", LiveID: 1,
		Clips0: []model.ClipRange{{StartTime: 10, EndTime: 5}},
	})
	if err == nil {
		t.Fatal("expected invalid clips0 error")
	}
}

// TestVideoProjectService_Create_LiveMaterialNotFound 验证关联素材不存在时返回错误。
func TestVideoProjectService_Create_LiveMaterialNotFound(t *testing.T) {
	svc := NewVideoProjectService(&mockVideoProjectRepo{}, &mockLiveMaterialRepoForProject{})
	_, err := svc.Create(context.Background(), 1, CreateVideoProjectInput{
		Name: "项目", LiveID: 9,
	})
	if err != ErrLiveMaterialNotFoundForProject {
		t.Errorf("Create() error = %v, want %v", err, ErrLiveMaterialNotFoundForProject)
	}
}

// TestVideoProjectService_Update_PartialFields 验证仅更新传入字段。
func TestVideoProjectService_Update_PartialFields(t *testing.T) {
	remark := "新备注"
	projectRepo := &mockVideoProjectRepo{
		projects: map[uint]*model.VideoProject{
			1: {ID: 1, Name: "旧名称", Remark: "旧备注", LiveID: 1, Clips0: []model.ClipRange{{StartTime: 0, EndTime: 1}}, Clips1: []model.ClipWithText{{Text: "旧", StartTime: 0, EndTime: 1}}, CreatedBy: 1},
		},
	}
	svc := NewVideoProjectService(projectRepo, &mockLiveMaterialRepoForProject{})

	updated, err := svc.Update(context.Background(), 1, VideoProjectUpdateInput{Remark: &remark})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Remark != remark {
		t.Errorf("Remark = %q, want %q", updated.Remark, remark)
	}
	if updated.Name != "旧名称" {
		t.Errorf("Name should remain unchanged, got %q", updated.Name)
	}
	if len(updated.Clips0) != 1 || updated.Clips0[0].EndTime != 1 {
		t.Errorf("Clips0 should remain unchanged, got %#v", updated.Clips0)
	}
}

// TestVideoProjectService_Update_EnableCaptions 验证可更新字幕开关。
func TestVideoProjectService_Update_EnableCaptions(t *testing.T) {
	projectRepo := &mockVideoProjectRepo{
		projects: map[uint]*model.VideoProject{
			1: {ID: 1, Name: "项目", LiveID: 1, EnableCaptions: model.EnableCaptionsOn, CreatedBy: 1},
		},
	}
	svc := NewVideoProjectService(projectRepo, &mockLiveMaterialRepoForProject{})
	off := model.EnableCaptionsOff
	updated, err := svc.Update(context.Background(), 1, VideoProjectUpdateInput{EnableCaptions: &off})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.EnableCaptions != model.EnableCaptionsOff {
		t.Errorf("EnableCaptions = %d, want 0", updated.EnableCaptions)
	}
}

// TestVideoProjectService_Update_ClipsWhenProvided 验证显式传入 clips 时才会更新。
func TestVideoProjectService_Update_ClipsWhenProvided(t *testing.T) {
	projectRepo := &mockVideoProjectRepo{
		projects: map[uint]*model.VideoProject{
			1: {ID: 1, Name: "项目", LiveID: 1, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{}, CreatedBy: 1},
		},
	}
	svc := NewVideoProjectService(projectRepo, &mockLiveMaterialRepoForProject{})

	clips0 := []model.ClipRange{{StartTime: 0, EndTime: 1000}}
	clips1 := []model.ClipWithText{{Text: "我是中国人", StartTime: 0, EndTime: 1000}}
	updated, err := svc.Update(context.Background(), 1, VideoProjectUpdateInput{
		Clips0: &clips0,
		Clips1: &clips1,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(updated.Clips0) != 1 || updated.Clips0[0].EndTime != 1000 {
		t.Errorf("Clips0 = %#v", updated.Clips0)
	}
	if len(updated.Clips1) != 1 || updated.Clips1[0].Text != "我是中国人" {
		t.Errorf("Clips1 = %#v", updated.Clips1)
	}

	// 传入空数组应清空切片。
	empty0 := []model.ClipRange{}
	cleared, err := svc.Update(context.Background(), 1, VideoProjectUpdateInput{Clips0: &empty0})
	if err != nil {
		t.Fatalf("clear clips0 error = %v", err)
	}
	if len(cleared.Clips0) != 0 {
		t.Errorf("Clips0 after clear = %#v, want empty", cleared.Clips0)
	}
	if len(cleared.Clips1) != 1 || cleared.Clips1[0].Text != "我是中国人" {
		t.Errorf("Clips1 should remain, got %#v", cleared.Clips1)
	}
}

// TestVideoProjectService_ListByLiveMaterial 验证按素材 ID 查询并计算分页 offset。
func TestVideoProjectService_ListByLiveMaterial(t *testing.T) {
	var gotFilter repository.VideoProjectListFilter
	var gotOffset, gotLimit int
	projectRepo := &mockVideoProjectRepo{
		listFn: func(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProjectListItem, int64, error) {
			gotFilter = filter
			gotOffset, gotLimit = offset, limit
			return []model.VideoProjectListItem{{ID: 3, Name: "关联项目", LiveID: 8}}, 1, nil
		},
	}
	liveRepo := &mockLiveMaterialRepoForProject{
		materials: map[uint]*model.LiveMaterial{8: {ID: 8, Name: "素材"}},
	}
	svc := NewVideoProjectService(projectRepo, liveRepo)

	items, total, err := svc.ListByLiveMaterial(context.Background(), 8, 2, 10)
	if err != nil {
		t.Fatalf("ListByLiveMaterial() error = %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "关联项目" {
		t.Fatalf("items/total = %+v/%d", items, total)
	}
	if gotFilter.LiveID == nil || *gotFilter.LiveID != 8 {
		t.Errorf("filter.LiveID = %v, want 8", gotFilter.LiveID)
	}
	if gotOffset != 10 || gotLimit != 10 {
		t.Errorf("offset/limit = %d/%d, want 10/10", gotOffset, gotLimit)
	}
}

// TestVideoProjectService_ListByLiveMaterial_NotFound 验证素材不存在时返回错误。
func TestVideoProjectService_ListByLiveMaterial_NotFound(t *testing.T) {
	svc := NewVideoProjectService(&mockVideoProjectRepo{}, &mockLiveMaterialRepoForProject{
		materials: map[uint]*model.LiveMaterial{},
	})
	_, _, err := svc.ListByLiveMaterial(context.Background(), 99, 1, 10)
	if err != ErrLiveMaterialNotFound {
		t.Errorf("ListByLiveMaterial() error = %v, want %v", err, ErrLiveMaterialNotFound)
	}
}

// TestVideoProjectService_ListByLiveMaterial_EmptyLiveID 验证 liveID 为 0 时拒绝查询。
func TestVideoProjectService_ListByLiveMaterial_EmptyLiveID(t *testing.T) {
	svc := NewVideoProjectService(&mockVideoProjectRepo{}, &mockLiveMaterialRepoForProject{})
	_, _, err := svc.ListByLiveMaterial(context.Background(), 0, 1, 10)
	if err == nil {
		t.Fatal("ListByLiveMaterial() error = nil, want 直播素材 ID 不能为空")
	}
}

// TestVideoProjectService_List_PassesFilter 验证列表筛选参数传递。
func TestVideoProjectService_List_PassesFilter(t *testing.T) {
	var gotFilter repository.VideoProjectListFilter
	projectRepo := &mockVideoProjectRepo{
		listFn: func(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProjectListItem, int64, error) {
			gotFilter = filter
			return nil, 0, nil
		},
	}
	svc := NewVideoProjectService(projectRepo, &mockLiveMaterialRepoForProject{})
	_, _, err := svc.List(context.Background(), 1, 10, VideoProjectListOptions{
		Keywords: "发布会", StartDate: "2026-01-01", EndDate: "2026-01-31",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(gotFilter.Keywords) != 1 || len(gotFilter.Keywords[0]) != 1 || gotFilter.Keywords[0][0] != "发布会" {
		t.Errorf("filter keywords = %v", gotFilter.Keywords)
	}
}

// TestVideoProjectService_Delete_NotFound 验证删除不存在记录时返回错误。
func TestVideoProjectService_Delete_NotFound(t *testing.T) {
	projectRepo := &mockVideoProjectRepo{
		deleteFn: func(ctx context.Context, id uint) error {
			return gorm.ErrRecordNotFound
		},
	}
	svc := NewVideoProjectService(projectRepo, &mockLiveMaterialRepoForProject{})
	if err := svc.Delete(context.Background(), 99); err != ErrVideoProjectNotFound {
		t.Errorf("Delete() error = %v, want %v", err, ErrVideoProjectNotFound)
	}
}
