package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Client 多对象存储客户端，对外提供统一的上传接口。
type Client struct {
	provider storageProvider
	basePath string
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
	return &Client{
		provider: provider,
		basePath: ResolveBasePath(cfg.BasePath),
	}, nil
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

// BasePath 返回对象键保存路径前缀（已规范化，不含首尾斜杠）。
func (c *Client) BasePath() string {
	return c.basePath
}

// ObjectKey 将相对路径片段拼接为完整对象键（自动附加 BasePath 前缀）。
func (c *Client) ObjectKey(parts ...string) string {
	key := strings.Join(parts, "/")
	return JoinObjectKey(c.basePath, key)
}

// TempObjectKey 生成临时文件对象键，位于 base_path/temp/ 下。
func (c *Client) TempObjectKey(parts ...string) string {
	return c.ObjectKey(append([]string{SubDirTemp}, parts...)...)
}

// TestObjectKey 生成测试文件对象键，位于 base_path/test/ 下（供 cmd/test 使用）。
func (c *Client) TestObjectKey(parts ...string) string {
	return c.ObjectKey(append([]string{SubDirTest}, parts...)...)
}

// UploadFile 将本地文件上传到对象存储。
//
// localPath 为本地文件路径，objectKey 为相对对象键名，会自动附加 BasePath 前缀。
// 返回上传完成后的带有效期签名访问 URL。
func (c *Client) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	if localPath == "" {
		return "", fmt.Errorf("本地文件路径不能为空")
	}
	if objectKey == "" {
		return "", fmt.Errorf("对象键名不能为空")
	}
	return c.provider.UploadFile(ctx, localPath, c.ObjectKey(objectKey))
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
	return c.provider.UploadReader(ctx, r, c.ObjectKey(objectKey), size)
}
