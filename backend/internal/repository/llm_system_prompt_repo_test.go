package repository

import (
	"context"
	"testing"
	"time"

	"live-mixer/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupLLMSystemPromptTestDB 创建内存 SQLite 数据库并迁移系统提示词表。
func setupLLMSystemPromptTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LLMSystemPrompt{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

// seedLLMSystemPrompt 向测试库插入一条系统提示词。
func seedLLMSystemPrompt(t *testing.T, db *gorm.DB, prompt *model.LLMSystemPrompt) *model.LLMSystemPrompt {
	t.Helper()
	// Create 回写后会把 is_editable 置为默认值 1，需提前保存期望值。
	wantEditable := prompt.IsEditable
	if err := db.Create(prompt).Error; err != nil {
		t.Fatalf("seed prompt: %v", err)
	}
	// GORM 会跳过 int8 零值，需用 map 显式写入 is_editable=0。
	if wantEditable == model.LLMSystemPromptNotEditable {
		if err := db.Model(prompt).Updates(map[string]interface{}{
			"is_editable": model.LLMSystemPromptNotEditable,
		}).Error; err != nil {
			t.Fatalf("update is_editable: %v", err)
		}
		prompt.IsEditable = model.LLMSystemPromptNotEditable
	}
	return prompt
}

// TestLLMSystemPromptRepository_CreateAndGetByID 验证创建与按 ID 查询。
func TestLLMSystemPromptRepository_CreateAndGetByID(t *testing.T) {
	db := setupLLMSystemPromptTestDB(t)
	repo := NewLLMSystemPromptRepository(db)
	ctx := context.Background()

	prompt := &model.LLMSystemPrompt{
		Name:       "测试提示词",
		Content:    "你是助手",
		Remark:     "备注",
		IsEditable: model.LLMSystemPromptEditable,
		CreatedBy:  1,
	}
	if err := repo.Create(ctx, prompt); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if prompt.ID == 0 {
		t.Fatal("Create() should set ID")
	}

	got, err := repo.GetByID(ctx, prompt.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Name != "测试提示词" {
		t.Errorf("Name = %q, want 测试提示词", got.Name)
	}
}

// TestLLMSystemPromptRepository_Update_OnlyUpdatesAllowedFields 验证仅更新允许编辑的字段。
func TestLLMSystemPromptRepository_Update_OnlyUpdatesAllowedFields(t *testing.T) {
	db := setupLLMSystemPromptTestDB(t)
	repo := NewLLMSystemPromptRepository(db)
	ctx := context.Background()

	prompt := seedLLMSystemPrompt(t, db, &model.LLMSystemPrompt{
		Name:       "旧名称",
		Content:    "旧内容",
		Remark:     "旧备注",
		IsEditable: model.LLMSystemPromptEditable,
		CreatedBy:  1,
	})

	prompt.Name = "新名称"
	prompt.Content = "新内容"
	prompt.Remark = "新备注"
	prompt.IsEditable = model.LLMSystemPromptNotEditable
	prompt.CreatedBy = 99
	if err := repo.Update(ctx, prompt); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.GetByID(ctx, prompt.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Name != "新名称" || got.Content != "新内容" || got.Remark != "新备注" {
		t.Errorf("unexpected updated fields: %+v", got)
	}
	if got.IsEditable != model.LLMSystemPromptEditable {
		t.Errorf("IsEditable = %d, want unchanged %d", got.IsEditable, model.LLMSystemPromptEditable)
	}
	if got.CreatedBy != 1 {
		t.Errorf("CreatedBy = %d, want unchanged 1", got.CreatedBy)
	}
}

// TestLLMSystemPromptRepository_Delete 验证物理删除。
func TestLLMSystemPromptRepository_Delete(t *testing.T) {
	db := setupLLMSystemPromptTestDB(t)
	repo := NewLLMSystemPromptRepository(db)
	ctx := context.Background()

	prompt := seedLLMSystemPrompt(t, db, &model.LLMSystemPrompt{
		Name:       "待删除",
		Content:    "内容",
		IsEditable: model.LLMSystemPromptEditable,
		CreatedBy:  1,
	})

	if err := repo.Delete(ctx, prompt.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repo.GetByID(ctx, prompt.ID); err == nil {
		t.Fatal("GetByID() after Delete should return error")
	}
}

// TestLLMSystemPromptRepository_List_KeywordFilter 验证关键词按「与」匹配 name/content/remark。
func TestLLMSystemPromptRepository_List_KeywordFilter(t *testing.T) {
	db := setupLLMSystemPromptTestDB(t)
	repo := NewLLMSystemPromptRepository(db)
	ctx := context.Background()

	seedLLMSystemPrompt(t, db, &model.LLMSystemPrompt{
		Name: "直播话术", Content: "周末促销内容", Remark: "默认", IsEditable: model.LLMSystemPromptEditable, CreatedBy: 1,
	})
	seedLLMSystemPrompt(t, db, &model.LLMSystemPrompt{
		Name: "直播开场", Content: "工作日内容", Remark: "备注", IsEditable: model.LLMSystemPromptEditable, CreatedBy: 1,
	})
	seedLLMSystemPrompt(t, db, &model.LLMSystemPrompt{
		Name: "商品介绍", Content: "周末优惠", Remark: "其它", IsEditable: model.LLMSystemPromptEditable, CreatedBy: 1,
	})

	prompts, total, err := repo.List(ctx, LLMSystemPromptListFilter{
		Keywords: KeywordGroups{{"直播", "周末"}},
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(prompts) != 1 || prompts[0].Name != "直播话术" {
		t.Errorf("unexpected result: total=%d prompts=%+v", total, prompts)
	}
}

// TestLLMSystemPromptRepository_List_DateFilter 验证按 created_at 日期范围筛选。
func TestLLMSystemPromptRepository_List_DateFilter(t *testing.T) {
	db := setupLLMSystemPromptTestDB(t)
	repo := NewLLMSystemPromptRepository(db)
	ctx := context.Background()

	inRange := &model.LLMSystemPrompt{
		Name: "范围内", Content: "内容", IsEditable: model.LLMSystemPromptEditable, CreatedBy: 1,
		CreatedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}
	outRange := &model.LLMSystemPrompt{
		Name: "范围外", Content: "内容", IsEditable: model.LLMSystemPromptEditable, CreatedBy: 1,
		CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, p := range []*model.LLMSystemPrompt{inRange, outRange} {
		if err := db.Create(p).Error; err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	startAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	prompts, total, err := repo.List(ctx, LLMSystemPromptListFilter{
		StartAt: &startAt,
		EndAt:   &endAt,
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(prompts) != 1 || prompts[0].Name != "范围内" {
		t.Errorf("unexpected result: total=%d prompts=%+v", total, prompts)
	}
}
