// envinit 是环境初始化 CLI 入口（建表、种子数据、重置密码）。
package main

import (
	"fmt"
	"os"

	"live-mixer/internal/bootstrap"
	"live-mixer/internal/config"
	"live-mixer/internal/migrator"
	"live-mixer/internal/seeder"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var configPath string

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "执行失败: %v\n", err)
		os.Exit(1)
	}
}

// newRootCmd 构建 envinit 根命令及全部子命令。
// 抽出函数便于单元测试校验命令注册与参数定义。
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "envinit",
		Short: "环境初始化工具（建表、种子数据、重置密码）",
	}

	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "外部配置文件路径（可选）")

	rootCmd.AddCommand(
		newSchemaCmd(),
		newSeedCmd(),
		newInitCmd(),
		newReinitCmd(),
		newResetPasswordCmd(),
	)

	return rootCmd
}

// newSchemaCmd 仅初始化数据库表结构。
func newSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "初始化数据库表结构",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(db *gorm.DB, logger *zap.Logger) error {
				return migrator.InitSchema(db, logger)
			})
		},
	}
}

// newSeedCmd 仅填充种子数据。
func newSeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "填充种子数据",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(db *gorm.DB, logger *zap.Logger) error {
				return seeder.SeedAll(db, logger)
			})
		},
	}
}

// newInitCmd 一键建表并填充种子数据（不清空已有表）。
func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "一键初始化：建表 + 填充种子数据",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(db *gorm.DB, logger *zap.Logger) error {
				if err := migrator.InitSchema(db, logger); err != nil {
					return err
				}
				return seeder.SeedAll(db, logger)
			})
		},
	}
}

// newReinitCmd 删除全部表与数据后，重新建表并填充默认种子数据。
func newReinitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reinit",
		Short: "重建环境：删除全部表与数据，再重新建表并填充默认数据",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(db *gorm.DB, logger *zap.Logger) error {
				// 先删表清数据，再重建 schema 与种子数据
				if err := migrator.DropAllTables(db, logger); err != nil {
					return err
				}
				if err := migrator.InitSchema(db, logger); err != nil {
					return err
				}
				return seeder.SeedAll(db, logger)
			})
		},
	}
}

// newResetPasswordCmd 重置账号密码（默认 admin）。
// 可通过 --username 指定账号；可通过 --password 指定新密码，未指定则生成随机密码。
func newResetPasswordCmd() *cobra.Command {
	var (
		username string
		password string
	)

	cmd := &cobra.Command{
		Use:   "reset-password",
		Short: "重置账号密码（默认 admin；未指定密码时生成 16 位随机密码）",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(db *gorm.DB, logger *zap.Logger) error {
				plain, err := seeder.ResetAccountPassword(db, logger, username, password)
				if err != nil {
					return err
				}
				// 同时向标准输出打印明文新密码，方便运维在终端直接查看
				fmt.Printf("账号 %s 的新密码: %s\n", usernameOrDefault(username), plain)
				return nil
			})
		},
	}

	cmd.Flags().StringVarP(&username, "username", "u", "admin", "要重置密码的账号用户名")
	cmd.Flags().StringVarP(&password, "password", "p", "", "新密码；为空时自动生成 16 位随机密码（0-9a-zA-Z）")
	return cmd
}

// usernameOrDefault 将空用户名归一为默认 admin（与 ResetAccountPassword 行为一致）。
func usernameOrDefault(username string) string {
	if username == "" {
		return "admin"
	}
	return username
}

// withDB 加载配置、初始化日志与数据库后执行业务函数。
func withDB(fn func(db *gorm.DB, logger *zap.Logger) error) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	logger, err := bootstrap.InitLogger(cfg.Logger)
	if err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	db, err := bootstrap.InitDatabase(cfg.Database, logger)
	if err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}

	return fn(db, logger)
}
