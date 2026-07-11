package storage

import (
	"context"
	"fmt"
	"io"
)

// Client 多对象存储客户端，对外提供统一的上传接口。
type Client struct {
	provider storageProvider
}

// NewClient 根据配置创建对象存储客户端。
// 当 COS、OSS、TOS 同时配置完整时，优先级为 COS > OSS > TOS。
func NewClient(cfg Config, opts ...UploadOptions) (*Client, error) {
	uploadOpts := UploadOptions{}
	if len(opts) > 0 {
		uploadOpts = opts[0]
	}

	provider, err := selectProvider(cfg, uploadOpts)
	if err != nil {
		return nil, err
	}
	return &Client{provider: provider}, nil
}

// NewClientFromEnv 从 COS_* / OSS_* / TOS_* 环境变量加载配置并创建客户端。
// 应用内请优先使用 config.Load 与 NewClientFromAppConfig。
func NewClientFromEnv(opts ...UploadOptions) (*Client, error) {
	return NewClient(LoadConfigFromEnv(), opts...)
}

// ProviderType 返回当前实际使用的对象存储后端类型。
func (c *Client) ProviderType() ProviderType {
	return c.provider.Type()
}

// UploadFile 将本地文件上传到对象存储。
//
// localPath 为本地文件路径，objectKey 为对象在存储桶中的键名（路径）。
// 返回上传完成后的访问 URL。
func (c *Client) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	if localPath == "" {
		return "", fmt.Errorf("本地文件路径不能为空")
	}
	if objectKey == "" {
		return "", fmt.Errorf("对象键名不能为空")
	}
	return c.provider.UploadFile(ctx, localPath, objectKey)
}

// UploadReader 将数据流上传到对象存储。
//
// size 为数据总字节数，未知时可传 -1（部分后端可能退化为缓冲上传）。
func (c *Client) UploadReader(ctx context.Context, r io.Reader, objectKey string, size int64) (string, error) {
	if r == nil {
		return "", fmt.Errorf("上传数据流不能为空")
	}
	if objectKey == "" {
		return "", fmt.Errorf("对象键名不能为空")
	}
	return c.provider.UploadReader(ctx, r, objectKey, size)
}
