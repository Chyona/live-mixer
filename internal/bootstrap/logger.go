// Package bootstrap 提供 webserver 与 envinit 共享的初始化逻辑。
package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"live-mixer/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	defaultLogFile      = "logs/base.log"
	defaultLogMaxSizeMB = 10
	defaultLogMaxBackup = 10
)

// InitLogger 根据配置初始化 Zap 日志实例，输出到轮转日志文件。
func InitLogger(cfg config.LoggerConfig) (*zap.Logger, error) {
	level := zapcore.InfoLevel
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	filename := cfg.Filename
	if filename == "" {
		filename = defaultLogFile
	}
	maxSize := cfg.MaxSize
	if maxSize <= 0 {
		maxSize = defaultLogMaxSizeMB
	}
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = defaultLogMaxBackup
	}

	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	var encoder zapcore.Encoder
	if strings.ToLower(cfg.Format) == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	}

	// 日志轮转：单文件最大 10MB，最多保留 10 个历史文件
	fileWriter := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		LocalTime:  true,
	}

	core := zapcore.NewCore(encoder, zapcore.AddSync(fileWriter), level)
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	return logger, nil
}
