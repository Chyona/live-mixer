package service

import (
	"context"
	"testing"

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
	listFn   func(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProject, int64, error)
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

func (m *mockVideoProjectRepo) List(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProject, int64, error) {
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
func (m *mockLiveMaterialRepoForProject) UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) UpdateASRProcessing(ctx context.Context, id uint) error { return nil }
func (m *mockLiveMaterialRepoForProject) UpdateASRProgress(ctx context.Context, id uint, progress int16) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) UpdateASRCompleted(ctx context.Context, id uint, liveASR string, duration int64) error {
	return nil
}
func (m *mockLiveMaterialRepoForProject) UpdateASRFailed(ctx context.Context, id uint, progress int16, errorMsg string) error {
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

	project, err := svc.Create(context.Background(), 2, "  项目  ", "备注", 1, "", "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if project.Name != "项目" {
		t.Errorf("Name = %q, want 项目", project.Name)
	}
	if project.Clips0 != "[]" || project.Clips1 != "[]" {
		t.Errorf("clips defaults = %q/%q, want []/[]", project.Clips0, project.Clips1)
	}
	if project.CreatedBy != 2 || project.LiveID != 1 {
		t.Errorf("unexpected project: %+v", project)
	}
}

// TestVideoProjectService_Create_LiveMaterialNotFound 验证关联素材不存在时返回错误。
func TestVideoProjectService_Create_LiveMaterialNotFound(t *testing.T) {
	svc := NewVideoProjectService(&mockVideoProjectRepo{}, &mockLiveMaterialRepoForProject{})
	_, err := svc.Create(context.Background(), 1, "项目", "", 9, "[]", "[]")
	if err != ErrLiveMaterialNotFoundForProject {
		t.Errorf("Create() error = %v, want %v", err, ErrLiveMaterialNotFoundForProject)
	}
}

// TestVideoProjectService_Update_PartialFields 验证仅更新传入字段。
func TestVideoProjectService_Update_PartialFields(t *testing.T) {
	draft := "https://draft.example.com"
	projectRepo := &mockVideoProjectRepo{
		projects: map[uint]*model.VideoProject{
			1: {ID: 1, Name: "旧名称", Remark: "旧备注", LiveID: 1, Clips0: "[]", Clips1: "[]", CreatedBy: 1},
		},
	}
	svc := NewVideoProjectService(projectRepo, &mockLiveMaterialRepoForProject{})

	updated, err := svc.Update(context.Background(), 1, VideoProjectUpdateInput{DraftURL: &draft})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.DraftURL != draft {
		t.Errorf("DraftURL = %q, want %q", updated.DraftURL, draft)
	}
	if updated.Name != "旧名称" {
		t.Errorf("Name should remain unchanged, got %q", updated.Name)
	}
}

// TestVideoProjectService_List_PassesFilter 验证列表筛选参数传递。
func TestVideoProjectService_List_PassesFilter(t *testing.T) {
	var gotFilter repository.VideoProjectListFilter
	projectRepo := &mockVideoProjectRepo{
		listFn: func(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProject, int64, error) {
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
	if len(gotFilter.Keywords) != 1 || gotFilter.Keywords[0] != "发布会" {
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

// TestValidateJSONClipArray 验证 clips JSON 数组校验。
func TestValidateJSONClipArray(t *testing.T) {
	if err := validateJSONClipArray("clips0", `[{"start_time":0,"end_time":10}]`); err != nil {
		t.Errorf("valid array should pass: %v", err)
	}
	if err := validateJSONClipArray("clips0", `{"start_time":0}`); err == nil {
		t.Error("object should fail validation")
	}
}
