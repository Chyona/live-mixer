package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

// tosProvider 火山引擎 TOS 存储后端实现。
type tosProvider struct {
	client     *tos.ClientV2
	bucketName string
	endpoint   string
	region     string
	opts       UploadOptions
}

// newTOSProvider 创建 TOS 存储后端。
func newTOSProvider(cfg TOSConfig, opts UploadOptions) (*tosProvider, error) {
	endpoint := resolveTOSEndpoint(cfg)
	client, err := tos.NewClientV2(endpoint,
		tos.WithRegion(cfg.Region),
		tos.WithCredentials(tos.NewStaticCredentials(cfg.AccessKeyID, cfg.AccessKeySecret)),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 TOS 客户端失败: %w", err)
	}

	return &tosProvider{
		client:     client,
		bucketName: cfg.BucketName,
		endpoint:   normalizeTOSHost(endpoint),
		region:     cfg.Region,
		opts:       opts,
	}, nil
}

// newTOSProviderWithClient 使用自定义客户端创建 TOS 后端，仅供单元测试注入 mock HTTP 服务。
func newTOSProviderWithClient(client *tos.ClientV2, bucketName, endpoint, region string, opts UploadOptions) *tosProvider {
	return &tosProvider{
		client:     client,
		bucketName: bucketName,
		endpoint:   normalizeTOSHost(endpoint),
		region:     region,
		opts:       opts,
	}
}

func (p *tosProvider) Type() ProviderType {
	return ProviderTOS
}

// objectURL 拼接上传完成后的对象访问地址（虚拟主机风格）。
func (p *tosProvider) objectURL(objectKey string) string {
	return fmt.Sprintf("https://%s.%s/%s", p.bucketName, p.endpoint, objectKey)
}

// uploadFileInput 构建 TOS 断点续传分片上传参数。
func (p *tosProvider) uploadFileInput(localPath, objectKey string) *tos.UploadFileInput {
	input := &tos.UploadFileInput{
		CreateMultipartUploadV2Input: tos.CreateMultipartUploadV2Input{
			Bucket: p.bucketName,
			Key:    objectKey,
		},
		FilePath: localPath,
		PartSize: p.opts.tosPartSizeBytes(),
		TaskNum:  p.opts.concurrency(),
	}
	if !p.opts.DisableCheckpoint {
		input.EnableCheckpoint = true
		input.CheckpointFile = tosCheckpointDir(p.opts)
	}
	return input
}

// UploadFile 使用 TOS 分片上传本地文件，并启用断点续传以应对网络中断。
func (p *tosProvider) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	if _, err := os.Stat(localPath); err != nil {
		return "", fmt.Errorf("读取本地文件失败: %w", err)
	}

	// TOS SDK 的 UploadFile 支持 context 取消，上传前先做快速检查。
	if err := ctx.Err(); err != nil {
		return "", err
	}

	_, err := p.client.UploadFile(ctx, p.uploadFileInput(localPath, objectKey))
	if err != nil {
		return "", fmt.Errorf("TOS 分片上传失败: %w", err)
	}
	return p.objectURL(objectKey), nil
}

// UploadReader 将数据流上传到 TOS；已知大小时走分片上传，否则退化为单次 PutObject。
func (p *tosProvider) UploadReader(ctx context.Context, r io.Reader, objectKey string, size int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// 数据量较大时先落盘，复用分片上传的断点续传能力。
	if size >= p.opts.tosPartSizeBytes() {
		tmpFile, err := os.CreateTemp(p.opts.CheckpointDir, "storage-tos-upload-*")
		if err != nil {
			return "", fmt.Errorf("创建临时文件失败: %w", err)
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		written, err := io.Copy(tmpFile, r)
		if closeErr := tmpFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			return "", fmt.Errorf("写入临时文件失败: %w", err)
		}
		if written != size {
			return "", fmt.Errorf("上传数据大小不匹配: 期望 %d 字节，实际 %d 字节", size, written)
		}
		return p.UploadFile(ctx, tmpPath, objectKey)
	}

	input := &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket:        p.bucketName,
			Key:           objectKey,
			ContentLength: size,
		},
		Content: r,
	}
	_, err := p.client.PutObjectV2(ctx, input)
	if err != nil {
		return "", fmt.Errorf("TOS 上传失败: %w", err)
	}
	return p.objectURL(objectKey), nil
}

// resolveTOSEndpoint 解析 TOS 访问端点；未显式配置时按地域生成默认域名。
func resolveTOSEndpoint(cfg TOSConfig) string {
	if cfg.Endpoint != "" {
		return cfg.Endpoint
	}
	return fmt.Sprintf("tos-%s.volces.com", cfg.Region)
}

// normalizeTOSHost 从端点字符串中提取主机名，用于拼接对象 URL。
func normalizeTOSHost(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	if idx := strings.Index(endpoint, "/"); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	return endpoint
}

// tosCheckpointDir 返回 TOS 断点续传目录，默认使用系统临时目录下的子目录。
func tosCheckpointDir(opts UploadOptions) string {
	if opts.CheckpointDir != "" {
		return filepath.Join(opts.CheckpointDir, "tos-checkpoint")
	}
	return filepath.Join(os.TempDir(), "storage-tos-checkpoint")
}
