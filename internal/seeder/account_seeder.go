package seeder

import (
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

	hashed, err := utils.HashPassword("admin")
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
			Status:   1,
		},
	}
	if err := db.Create(&accounts).Error; err != nil {
		return err
	}
	logger.Info("账号种子数据填充成功", zap.Int("count", len(accounts)))
	return nil
}
