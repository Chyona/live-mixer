// envinit 是环境初始化 CLI 入口（建表、种子数据）。
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
	rootCmd := &cobra.Command{
		Use:   "envinit",
		Short: "环境初始化工具（建表、种子数据）",
	}

	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "外部配置文件路径（可选）")

	rootCmd.AddCommand(
		&cobra.Command{
			Use:   "schema",
			Short: "初始化数据库表结构",
			RunE: func(cmd *cobra.Command, args []string) error {
				return withDB(func(db *gorm.DB, logger *zap.Logger) error {
					return migrator.InitSchema(db, logger)
				})
			},
		},
		&cobra.Command{
			Use:   "seed",
			Short: "填充种子数据",
			RunE: func(cmd *cobra.Command, args []string) error {
				return withDB(func(db *gorm.DB, logger *zap.Logger) error {
					return seeder.SeedAll(db, logger)
				})
			},
		},
		&cobra.Command{
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
		},
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "执行失败: %v\n", err)
		os.Exit(1)
	}
}

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
