package service

import (
	"context"
	"errors"
	"testing"

	"live-mixer/internal/model"

	"gorm.io/gorm"
)

// mockLiveMaterialRepo 用于直播素材 service 单元测试的仓储 mock。
type mockLiveMaterialRepo struct {
	materials map[uint]*model.LiveMaterial
	nextID    uint
	createFn  func(ctx context.Context, material *model.LiveMaterial) error
	updateFn  func(ctx context.Context, material *model.LiveMaterial) error
	listFn    func(ctx context.Context, offset, limit int) ([]model.LiveMaterialListItem, int64, error)
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

func (m *mockLiveMaterialRepo) List(ctx context.Context, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, offset, limit)
	}
	return nil, 0, nil
}

// TestLiveMaterialService_Create_Success 验证创建时写入默认值且创建人正确。
func TestLiveMaterialService_Create_Success(t *testing.T) {
	repo := &mockLiveMaterialRepo{}
	svc := NewLiveMaterialService(repo)

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
	svc := NewLiveMaterialService(&mockLiveMaterialRepo{})
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
	svc := NewLiveMaterialService(&mockLiveMaterialRepo{})
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
	svc := NewLiveMaterialService(repo)

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
	svc := NewLiveMaterialService(&mockLiveMaterialRepo{materials: map[uint]*model.LiveMaterial{}})
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
	svc := NewLiveMaterialService(repo)

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
	svc := NewLiveMaterialService(repo)
	_, err := svc.Create(context.Background(), 1, "素材", "https://example.com/a.mp4", "", "")
	if err == nil || err.Error() != "db down" {
		t.Errorf("error = %v, want db down", err)
	}
}

// TestLiveMaterialService_List_Pagination 验证分页 offset 计算正确并返回仓储结果。
func TestLiveMaterialService_List_Pagination(t *testing.T) {
	var gotOffset, gotLimit int
	repo := &mockLiveMaterialRepo{
		listFn: func(ctx context.Context, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
			gotOffset = offset
			gotLimit = limit
			return []model.LiveMaterialListItem{
				{ID: 2, Name: "素材B", ASRStatus: model.ASRStatusCompleted},
			}, 5, nil
		},
	}
	svc := NewLiveMaterialService(repo)

	materials, total, err := svc.List(context.Background(), 2, 20)
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
