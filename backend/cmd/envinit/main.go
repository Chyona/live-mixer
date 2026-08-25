// 数据库建表、种子数据与重置密码入口。
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

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var configPath string
	root := &cobra.Command{
		Use:   "envinit",
		Short: "初始化 live-mixer 数据库（建表 / 种子数据 / 重置密码）",
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "外部配置文件路径（可选；否则用内嵌 config + 环境变量）")

	withDB := func(run func(db *gorm.DB, logger *zap.Logger) error) func(*cobra.Command, []string) error {
		return func(*cobra.Command, []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			logger, err := bootstrap.InitLogger(cfg.Logger)
			if err != nil {
				return err
			}
			defer logger.Sync() //nolint:errcheck

			db, err := bootstrap.InitDatabase(cfg.Database, logger)
			if err != nil {
				return err
			}
			return run(db, logger)
		}
	}

	root.AddCommand(&cobra.Command{
		Use:   "schema",
		Short: "仅初始化数据库表结构",
		RunE: withDB(func(db *gorm.DB, logger *zap.Logger) error {
			return migrator.InitSchema(db, logger)
		}),
	})
	root.AddCommand(&cobra.Command{
		Use:   "seed",
		Short: "仅填充种子数据",
		RunE: withDB(func(db *gorm.DB, logger *zap.Logger) error {
			return seeder.SeedAll(db, logger)
		}),
	})
	root.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "建表并填充种子数据",
		RunE: withDB(func(db *gorm.DB, logger *zap.Logger) error {
			if err := migrator.InitSchema(db, logger); err != nil {
				return err
			}
			return seeder.SeedAll(db, logger)
		}),
	})
	root.AddCommand(&cobra.Command{
		Use:   "reinit",
		Short: "删除全部业务表后重新建表并填充种子数据",
		RunE: withDB(func(db *gorm.DB, logger *zap.Logger) error {
			if err := migrator.DropAllTables(db, logger); err != nil {
				return err
			}
			if err := migrator.InitSchema(db, logger); err != nil {
				return err
			}
			return seeder.SeedAll(db, logger)
		}),
	})

	var username, password string
	resetCmd := &cobra.Command{
		Use:   "reset-password",
		Short: "重置指定账号登录密码（默认 admin）",
		RunE: withDB(func(db *gorm.DB, logger *zap.Logger) error {
			_, err := seeder.ResetAccountPassword(db, logger, username, password)
			return err
		}),
	}
	resetCmd.Flags().StringVarP(&username, "username", "u", "admin", "目标账号用户名")
	resetCmd.Flags().StringVarP(&password, "password", "p", "", "新明文密码；为空则自动生成")
	root.AddCommand(resetCmd)

	return root
}
