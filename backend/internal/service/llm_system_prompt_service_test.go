package service

import (
	"context"
	"strings"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// mockLLMSystemPromptRepo 用于系统提示词 service 单元测试的仓储 mock。
type mockLLMSystemPromptRepo struct {
	prompts  map[uint]*model.LLMSystemPrompt
	nextID   uint
	createFn func(ctx context.Context, prompt *model.LLMSystemPrompt) error
	updateFn func(ctx context.Context, prompt *model.LLMSystemPrompt) error
	deleteFn func(ctx context.Context, id uint) error
	listFn   func(ctx context.Context, filter repository.LLMSystemPromptListFilter, offset, limit int) ([]model.LLMSystemPrompt, int64, error)
}

func (m *mockLLMSystemPromptRepo) Create(ctx context.Context, prompt *model.LLMSystemPrompt) error {
	if m.createFn != nil {
		return m.createFn(ctx, prompt)
	}
	m.nextID++
	prompt.ID = m.nextID
	if m.prompts == nil {
		m.prompts = make(map[uint]*model.LLMSystemPrompt)
	}
	stored := *prompt
	m.prompts[prompt.ID] = &stored
	return nil
}

func (m *mockLLMSystemPromptRepo) GetByID(ctx context.Context, id uint) (*model.LLMSystemPrompt, error) {
	prompt, ok := m.prompts[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	stored := *prompt
	return &stored, nil
}

func (m *mockLLMSystemPromptRepo) Update(ctx context.Context, prompt *model.LLMSystemPrompt) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, prompt)
	}
	existing, ok := m.prompts[prompt.ID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	existing.Name = prompt.Name
	existing.Content = prompt.Content
	existing.Remark = prompt.Remark
	existing.Ext = prompt.Ext
	return nil
}

func (m *mockLLMSystemPromptRepo) Delete(ctx context.Context, id uint) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	delete(m.prompts, id)
	return nil
}

func (m *mockLLMSystemPromptRepo) List(ctx context.Context, filter repository.LLMSystemPromptListFilter, offset, limit int) ([]model.LLMSystemPrompt, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter, offset, limit)
	}
	return nil, 0, nil
}

// TestLLMSystemPromptService_Create_Success 验证创建时写入默认值且创建人正确。
func TestLLMSystemPromptService_Create_Success(t *testing.T) {
	repo := &mockLLMSystemPromptRepo{}
	svc := NewLLMSystemPromptService(repo)

	prompt, err := svc.Create(context.Background(), 3, "  名称  ", "  内容  ", "备注", "ext")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if prompt.Name != "名称" {
		t.Errorf("Name = %q, want 名称", prompt.Name)
	}
	if prompt.Content != "内容" {
		t.Errorf("Content = %q, want 内容", prompt.Content)
	}
	if prompt.IsEditable != model.LLMSystemPromptEditable {
		t.Errorf("IsEditable = %d, want %d", prompt.IsEditable, model.LLMSystemPromptEditable)
	}
	if prompt.CreatedBy != 3 {
		t.Errorf("CreatedBy = %d, want 3", prompt.CreatedBy)
	}
}

// TestLLMSystemPromptService_Create_EmptyName 验证名称为空时返回错误。
func TestLLMSystemPromptService_Create_EmptyName(t *testing.T) {
	svc := NewLLMSystemPromptService(&mockLLMSystemPromptRepo{})
	if _, err := svc.Create(context.Background(), 1, "   ", "内容", "", ""); err == nil {
		t.Fatal("Create() should fail for empty name")
	}
}

// TestLLMSystemPromptService_Update_NotEditable 验证预置提示词不可修改。
func TestLLMSystemPromptService_Update_NotEditable(t *testing.T) {
	repo := &mockLLMSystemPromptRepo{
		prompts: map[uint]*model.LLMSystemPrompt{
			1: {
				ID: 1, Name: "预置", Content: "内容",
				IsEditable: model.LLMSystemPromptNotEditable, CreatedBy: 1,
			},
		},
	}
	svc := NewLLMSystemPromptService(repo)

	_, err := svc.Update(context.Background(), 1, "新名称", "新内容", "", "")
	if err != ErrLLMSystemPromptNotEditable {
		t.Errorf("Update() error = %v, want %v", err, ErrLLMSystemPromptNotEditable)
	}
}

// TestLLMSystemPromptService_Update_NotFound 验证记录不存在时返回错误。
func TestLLMSystemPromptService_Update_NotFound(t *testing.T) {
	svc := NewLLMSystemPromptService(&mockLLMSystemPromptRepo{prompts: map[uint]*model.LLMSystemPrompt{}})
	_, err := svc.Update(context.Background(), 99, "名称", "内容", "", "")
	if err != ErrLLMSystemPromptNotFound {
		t.Errorf("Update() error = %v, want %v", err, ErrLLMSystemPromptNotFound)
	}
}

// TestLLMSystemPromptService_Delete_NotDeletable 验证预置提示词不可删除。
func TestLLMSystemPromptService_Delete_NotDeletable(t *testing.T) {
	repo := &mockLLMSystemPromptRepo{
		prompts: map[uint]*model.LLMSystemPrompt{
			1: {ID: 1, Name: "预置", Content: "内容", IsEditable: model.LLMSystemPromptNotEditable},
		},
	}
	svc := NewLLMSystemPromptService(repo)

	if err := svc.Delete(context.Background(), 1); err != ErrLLMSystemPromptNotDeletable {
		t.Errorf("Delete() error = %v, want %v", err, ErrLLMSystemPromptNotDeletable)
	}
}

// TestLLMSystemPromptService_List_FullContent 验证列表返回完整 content。
func TestLLMSystemPromptService_List_FullContent(t *testing.T) {
	fullContent := strings.Repeat("中", 120)
	repo := &mockLLMSystemPromptRepo{
		listFn: func(ctx context.Context, filter repository.LLMSystemPromptListFilter, offset, limit int) ([]model.LLMSystemPrompt, int64, error) {
			return []model.LLMSystemPrompt{
				{ID: 1, Name: "测试", Content: fullContent, IsEditable: 1, CreatedBy: 1},
			}, 1, nil
		},
	}
	svc := NewLLMSystemPromptService(repo)

	items, total, err := svc.List(context.Background(), 1, 20, LLMSystemPromptListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("unexpected list result: total=%d len=%d", total, len(items))
	}
	if items[0].Content != fullContent {
		t.Errorf("content = %q, want full content", items[0].Content)
	}
}

// TestLLMSystemPromptService_Get_NotFound 验证详情不存在时返回错误。
func TestLLMSystemPromptService_Get_NotFound(t *testing.T) {
	svc := NewLLMSystemPromptService(&mockLLMSystemPromptRepo{prompts: map[uint]*model.LLMSystemPrompt{}})
	if _, err := svc.Get(context.Background(), 1); err != ErrLLMSystemPromptNotFound {
		t.Errorf("Get() error = %v, want %v", err, ErrLLMSystemPromptNotFound)
	}
}

// TestBuildLLMSystemPromptListFilter 验证日期与关键词筛选条件构建。
func TestBuildLLMSystemPromptListFilter(t *testing.T) {
	filter, err := buildLLMSystemPromptListFilter(LLMSystemPromptListOptions{
		Keywords:  "直播,话术",
		StartDate: "2026-01-01",
		EndDate:   "2026-01-31",
	})
	if err != nil {
		t.Fatalf("buildLLMSystemPromptListFilter() error = %v", err)
	}
	if filter.StartAt == nil || filter.EndAt == nil {
		t.Fatal("date range should be set")
	}
	if len(filter.Keywords) != 1 || len(filter.Keywords[0]) != 2 {
		t.Fatalf("keywords = %#v, want one AND group with 2 terms", filter.Keywords)
	}
}

// TestBuildLLMSystemPromptListFilter_InvalidDate 验证非法日期返回错误。
func TestBuildLLMSystemPromptListFilter_InvalidDate(t *testing.T) {
	_, err := buildLLMSystemPromptListFilter(LLMSystemPromptListOptions{StartDate: "2026/01/01"})
	if err == nil {
		t.Fatal("expected invalid date error")
	}
}

// TestLLMSystemPromptService_List_PassesFilter 验证列表查询将筛选条件传递给仓储层。
func TestLLMSystemPromptService_List_PassesFilter(t *testing.T) {
	var gotFilter repository.LLMSystemPromptListFilter
	repo := &mockLLMSystemPromptRepo{
		listFn: func(ctx context.Context, filter repository.LLMSystemPromptListFilter, offset, limit int) ([]model.LLMSystemPrompt, int64, error) {
			gotFilter = filter
			return nil, 0, nil
		},
	}
	svc := NewLLMSystemPromptService(repo)
	_, _, err := svc.List(context.Background(), 1, 10, LLMSystemPromptListOptions{Keywords: "直播"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(gotFilter.Keywords) != 1 || len(gotFilter.Keywords[0]) != 1 || gotFilter.Keywords[0][0] != "直播" {
		t.Errorf("filter = %+v", gotFilter)
	}
}
