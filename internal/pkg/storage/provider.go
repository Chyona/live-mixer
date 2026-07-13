package storage

import (
	"context"
	"fmt"
	"io"
)

// ProviderType 标识当前使用的对象存储后端。
type ProviderType string

const (
	// ProviderCOS 腾讯云对象存储。
	ProviderCOS ProviderType = "cos"
	// ProviderOSS 阿里云对象存储。
	ProviderOSS ProviderType = "oss"
	// ProviderTOS 火山引擎对象存储。
	ProviderTOS ProviderType = "tos"
)

// storageProvider 对象存储后端抽象，便于在 COS / OSS / TOS 之间切换并编写单元测试。
type storageProvider interface {
	// UploadFile 将本地文件上传到对象存储，内部使用分片上传以应对弱网环境。
	UploadFile(ctx context.Context, localPath, objectKey string) (string, error)
	// UploadReader 将数据流上传到对象存储，适用于内存或管道数据源。
	UploadReader(ctx context.Context, r io.Reader, objectKey string, size int64) (string, error)
	// Type 返回当前后端类型。
	Type() ProviderType
}

// selectProvider 根据配置选择对象存储后端，优先级为 COS > OSS > TOS。
func selectProvider(cfg Config, opts UploadOptions) (storageProvider, error) {
	if isCOSConfigured(cfg.COS) {
		provider, err := newCOSProvider(cfg.COS, opts, cfg.SignedURLExpireDays)
		if err != nil {
			return nil, fmt.Errorf("初始化 COS 客户端失败: %w", err)
		}
		return provider, nil
	}
	if isOSSConfigured(cfg.OSS) {
		provider, err := newOSSProvider(cfg.OSS, opts, cfg.SignedURLExpireDays)
		if err != nil {
			return nil, fmt.Errorf("初始化 OSS 客户端失败: %w", err)
		}
		return provider, nil
	}
	if isTOSConfigured(cfg.TOS) {
		provider, err := newTOSProvider(cfg.TOS, opts, cfg.SignedURLExpireDays)
		if err != nil {
			return nil, fmt.Errorf("初始化 TOS 客户端失败: %w", err)
		}
		return provider, nil
	}
	return nil, fmt.Errorf("未配置可用的对象存储，请设置 COS、OSS 或 TOS 环境变量")
}
