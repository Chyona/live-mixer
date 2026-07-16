package seeder

import (
	"strings"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/pkg/utils"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
)

// setupSeederTestDB 创建内存库并迁移账号与系统提示词表。
func setupSeederTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Account{}, &model.LLMSystemPrompt{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

// TestSeedAccounts 验证空表会写入默认 admin，且已有数据时跳过。
func TestSeedAccounts(t *testing.T) {
	db := setupSeederTestDB(t)
	logger := zap.NewNop()

	if err := SeedAccounts(db, logger); err != nil {
		t.Fatalf("SeedAccounts() error = %v", err)
	}

	var count int64
	if err := db.Model(&model.Account{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	var admin model.Account
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatalf("find admin: %v", err)
	}
	if !utils.ComparePassword(admin.Password, "admin") {
		t.Error("默认管理员密码应为 admin")
	}

	// 再次 seed 应跳过，不增加记录
	if err := SeedAccounts(db, logger); err != nil {
		t.Fatalf("SeedAccounts() second call error = %v", err)
	}
	if err := db.Model(&model.Account{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("第二次 seed 后 count = %d, want 1", count)
	}
}

// TestSeedAll 验证 SeedAll 能正常完成账号与系统提示词种子填充。
func TestSeedAll(t *testing.T) {
	db := setupSeederTestDB(t)
	logger := zap.NewNop()

	if err := SeedAll(db, logger); err != nil {
		t.Fatalf("SeedAll() error = %v", err)
	}
	var accountCount int64
	if err := db.Model(&model.Account{}).Count(&accountCount).Error; err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if accountCount != 1 {
		t.Fatalf("account count = %d, want 1", accountCount)
	}
	var promptCount int64
	if err := db.Model(&model.LLMSystemPrompt{}).Count(&promptCount).Error; err != nil {
		t.Fatalf("count prompts: %v", err)
	}
	if promptCount != 1 {
		t.Fatalf("prompt count = %d, want 1", promptCount)
	}
}

// TestResetAccountPassword_Specified 验证使用指定密码重置。
func TestResetAccountPassword_Specified(t *testing.T) {
	db := setupSeederTestDB(t)
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	if err := SeedAccounts(db, logger); err != nil {
		t.Fatalf("SeedAccounts: %v", err)
	}

	const newPwd = "MyNewPass123456"
	got, err := ResetAccountPassword(db, logger, "admin", newPwd)
	if err != nil {
		t.Fatalf("ResetAccountPassword() error = %v", err)
	}
	if got != newPwd {
		t.Fatalf("返回密码 = %q, want %q", got, newPwd)
	}

	var admin model.Account
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatalf("find admin: %v", err)
	}
	if !utils.ComparePassword(admin.Password, newPwd) {
		t.Error("数据库中的密码哈希与指定新密码不匹配")
	}

	// 确认日志中明文输出新密码（未脱敏）
	assertPasswordLogged(t, logs, "admin", newPwd)
}

// TestResetAccountPassword_Random 验证未指定密码时生成 16 位随机密码并写入日志。
func TestResetAccountPassword_Random(t *testing.T) {
	db := setupSeederTestDB(t)
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	if err := SeedAccounts(db, zap.NewNop()); err != nil {
		t.Fatalf("SeedAccounts: %v", err)
	}

	got, err := ResetAccountPassword(db, logger, "admin", "")
	if err != nil {
		t.Fatalf("ResetAccountPassword() error = %v", err)
	}
	if len(got) != utils.DefaultRandomPasswordLength {
		t.Fatalf("随机密码长度 = %d, want %d", len(got), utils.DefaultRandomPasswordLength)
	}

	var admin model.Account
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatalf("find admin: %v", err)
	}
	if !utils.ComparePassword(admin.Password, got) {
		t.Error("数据库中的密码哈希与返回的随机密码不匹配")
	}

	assertPasswordLogged(t, logs, "admin", got)
}

// TestResetAccountPassword_CustomUsername 验证可指定非 admin 账号名。
func TestResetAccountPassword_CustomUsername(t *testing.T) {
	db := setupSeederTestDB(t)
	logger := zap.NewNop()

	hashed, _ := utils.HashPassword("old")
	if err := db.Create(&model.Account{
		Username: "demo",
		Email:    "demo@example.com",
		Password: hashed,
		IsActive: 1,
	}).Error; err != nil {
		t.Fatalf("create demo: %v", err)
	}

	const newPwd = "DemoPass00000001"
	got, err := ResetAccountPassword(db, logger, "demo", newPwd)
	if err != nil {
		t.Fatalf("ResetAccountPassword() error = %v", err)
	}
	if got != newPwd {
		t.Fatalf("返回密码 = %q, want %q", got, newPwd)
	}

	var demo model.Account
	if err := db.Where("username = ?", "demo").First(&demo).Error; err != nil {
		t.Fatalf("find demo: %v", err)
	}
	if !utils.ComparePassword(demo.Password, newPwd) {
		t.Error("demo 密码未正确更新")
	}
}

// TestResetAccountPassword_DefaultUsername 验证 username 为空时默认重置 admin。
func TestResetAccountPassword_DefaultUsername(t *testing.T) {
	db := setupSeederTestDB(t)
	logger := zap.NewNop()

	if err := SeedAccounts(db, logger); err != nil {
		t.Fatalf("SeedAccounts: %v", err)
	}

	got, err := ResetAccountPassword(db, logger, "", "ExplicitPwdabcdef")
	if err != nil {
		t.Fatalf("ResetAccountPassword() error = %v", err)
	}

	var admin model.Account
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatalf("find admin: %v", err)
	}
	if !utils.ComparePassword(admin.Password, got) {
		t.Error("空 username 应默认重置 admin")
	}
}

// TestResetAccountPassword_NotFound 验证账号不存在时返回错误。
func TestResetAccountPassword_NotFound(t *testing.T) {
	db := setupSeederTestDB(t)
	logger := zap.NewNop()

	_, err := ResetAccountPassword(db, logger, "ghost", "whatever")
	if err == nil {
		t.Fatal("expected error for missing account")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error = %v, want mention username", err)
	}
}

// assertPasswordLogged 断言日志中以明文输出了新密码（字段名 new_password）。
func assertPasswordLogged(t *testing.T, logs *observer.ObservedLogs, username, password string) {
	t.Helper()
	entries := logs.FilterMessage("账号密码已重置").All()
	if len(entries) == 0 {
		t.Fatal("未找到密码重置日志")
	}
	entry := entries[len(entries)-1]
	ctx := entry.ContextMap()
	if ctx["username"] != username {
		t.Errorf("log username = %v, want %q", ctx["username"], username)
	}
	if ctx["new_password"] != password {
		t.Errorf("log new_password = %v, want 明文 %q（不可脱敏）", ctx["new_password"], password)
	}
}
