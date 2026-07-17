package seeder

import (
	"fmt"

	"live-mixer/internal/model"
	"live-mixer/pkg/utils"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SeedAccounts 填充默认账号种子数据。
func SeedAccounts(db *gorm.DB, logger *zap.Logger) error {
	var count int64
	if err := db.Model(&model.Account{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		logger.Info("账号表已有数据，跳过种子填充", zap.Int64("count", count))
		return nil
	}

	hashed, err := utils.HashPassword("Aa123456")
	if err != nil {
		return err
	}

	accounts := []model.Account{
		{
			Username: "admin",
			Email:    "admin@example.com",
			Password: hashed,
			Nickname: "管理员",
			Roles:    "ADMIN", // 默认管理员角色
			IsActive: 1,
		},
	}
	if err := db.Create(&accounts).Error; err != nil {
		return err
	}
	logger.Info("账号种子数据填充成功", zap.Int("count", len(accounts)))
	return nil
}

// ResetAccountPassword 重置指定账号的登录密码。
//
// 参数说明：
//   - username：目标账号用户名；为空时默认使用 "admin"
//   - password：新明文密码；为空时自动生成长度为 16 的随机密码（字符集 0-9a-zA-Z）
//
// 返回值为最终使用的明文新密码（调用方必须原样写入日志，禁止脱敏）。
func ResetAccountPassword(db *gorm.DB, logger *zap.Logger, username, password string) (string, error) {
	if username == "" {
		username = "admin"
	}

	// 未指定密码时生成随机密码
	plain := password
	if plain == "" {
		generated, err := utils.GenerateRandomPassword(utils.DefaultRandomPasswordLength)
		if err != nil {
			return "", fmt.Errorf("生成随机密码失败: %w", err)
		}
		plain = generated
	}

	hashed, err := utils.HashPassword(plain)
	if err != nil {
		return "", fmt.Errorf("密码哈希失败: %w", err)
	}

	result := db.Model(&model.Account{}).Where("username = ?", username).Update("password", hashed)
	if result.Error != nil {
		return "", fmt.Errorf("更新密码失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("账号不存在: %s", username)
	}

	// 明文新密码写入日志，禁止脱敏（运维重置密码必须可见）
	logger.Info("账号密码已重置",
		zap.String("username", username),
		zap.String("new_password", plain),
	)
	return plain, nil
}
