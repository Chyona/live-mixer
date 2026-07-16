package seeder

import (
	"testing"

	"live-mixer/internal/model"

	"go.uber.org/zap"
)

// TestSeedLLMSystemPrompts 验证空表写入默认提示词，且已有数据时跳过。
func TestSeedLLMSystemPrompts(t *testing.T) {
	db := setupSeederTestDB(t)
	logger := zap.NewNop()

	if err := SeedAccounts(db, logger); err != nil {
		t.Fatalf("SeedAccounts: %v", err)
	}
	if err := SeedLLMSystemPrompts(db, logger); err != nil {
		t.Fatalf("SeedLLMSystemPrompts() error = %v", err)
	}

	var count int64
	if err := db.Model(&model.LLMSystemPrompt{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	var prompt model.LLMSystemPrompt
	if err := db.First(&prompt).Error; err != nil {
		t.Fatalf("find prompt: %v", err)
	}
	if prompt.ID != model.DefaultVideoProjectPromptID {
		t.Errorf("ID = %d, want %d（项目默认 prompt_id）", prompt.ID, model.DefaultVideoProjectPromptID)
	}
	if prompt.Name != defaultLLMSystemPromptName {
		t.Errorf("Name = %q, want %q", prompt.Name, defaultLLMSystemPromptName)
	}
	if prompt.Content != defaultLLMSystemPromptContent {
		t.Errorf("Content 与默认常量不一致，len=%d want=%d", len(prompt.Content), len(defaultLLMSystemPromptContent))
	}
	if prompt.Remark != defaultLLMSystemPromptRemark {
		t.Errorf("Remark = %q, want %q", prompt.Remark, defaultLLMSystemPromptRemark)
	}
	if prompt.IsEditable != model.LLMSystemPromptNotEditable {
		t.Errorf("IsEditable = %d, want %d", prompt.IsEditable, model.LLMSystemPromptNotEditable)
	}
	if prompt.CreatedBy == 0 {
		t.Error("CreatedBy 不应为 0")
	}

	// 再次 seed 应跳过，不增加记录
	if err := SeedLLMSystemPrompts(db, logger); err != nil {
		t.Fatalf("SeedLLMSystemPrompts() second call error = %v", err)
	}
	if err := db.Model(&model.LLMSystemPrompt{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("第二次 seed 后 count = %d, want 1", count)
	}
}

// TestSeedLLMSystemPrompts_RequiresAdmin 验证无管理员账号时返回错误。
func TestSeedLLMSystemPrompts_RequiresAdmin(t *testing.T) {
	db := setupSeederTestDB(t)
	logger := zap.NewNop()

	err := SeedLLMSystemPrompts(db, logger)
	if err == nil {
		t.Fatal("expected error when admin account missing")
	}
}
