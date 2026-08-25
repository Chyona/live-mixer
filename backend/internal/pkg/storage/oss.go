package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// ossProvider 阿里云 OSS 存储后端实现。
type ossProvider struct {
	bucket              *oss.Bucket
	bucketName          string
	endpoint            string
	opts                UploadOptions
	signedURLExpireDays int
}

// newOSSProvider 创建 OSS 存储后端。
func newOSSProvider(cfg OSSConfig, opts UploadOptions, signedURLExpireDays int) (*ossProvider, error) {
	client, err := oss.New(cfg.Endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("创建 OSS 客户端失败: %w", err)
	}

	bucket, err := client.Bucket(cfg.BucketName)
	if err != nil {
		return nil, fmt.Errorf("获取 OSS Bucket 失败: %w", err)
	}

	return &ossProvider{
		bucket:              bucket,
		bucketName:          cfg.BucketName,
		endpoint:            cfg.Endpoint,
		opts:                opts,
		signedURLExpireDays: signedURLExpireDays,
	}, nil
}

// newOSSProviderWithBucket 使用自定义 Bucket 创建 OSS 后端，仅供单元测试注入 mock HTTP 服务。
func newOSSProviderWithBucket(bucket *oss.Bucket, bucketName, endpoint string, opts UploadOptions, signedURLExpireDays int) *ossProvider {
	return &ossProvider{
		bucket:              bucket,
		bucketName:          bucketName,
		endpoint:            endpoint,
		opts:                opts,
		signedURLExpireDays: signedURLExpireDays,
	}
}

func (p *ossProvider) Type() ProviderType {
	return ProviderOSS
}

// objectURL 拼接对象的公开访问地址（未签名）。
func (p *ossProvider) objectURL(objectKey string) string {
	return fmt.Sprintf("https://%s.%s/%s", p.bucketName, p.endpoint, objectKey)
}

// presignedURL 生成带有效期的 GET 签名链接。
func (p *ossProvider) presignedURL(objectKey string) (string, error) {
	expireSec := int64(signedURLExpireDuration(p.signedURLExpireDays, ProviderOSS).Seconds())
	url, err := p.bucket.SignURL(objectKey, oss.HTTPGet, expireSec)
	if err != nil {
		return "", fmt.Errorf("OSS 生成签名 URL 失败: %w", err)
	}
	return url, nil
}

// accessURL 返回上传完成后的可访问链接（默认带签名）。
func (p *ossProvider) accessURL(objectKey string) (string, error) {
	return p.presignedURL(objectKey)
}

// uploadOptions 构建 OSS 分片上传选项，启用断点续传与并发分片。
func (p *ossProvider) uploadOptions() []oss.Option {
	opts := []oss.Option{
		oss.Routines(p.opts.concurrency()),
	}
	if !p.opts.DisableCheckpoint {
		opts = append(opts, oss.Checkpoint(true, ossCheckpointDir(p.opts)))
	}
	return opts
}

// UploadFile 使用 OSS 分片上传本地文件，并启用断点续传以应对网络中断。
func (p *ossProvider) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	if _, err := os.Stat(localPath); err != nil {
		return "", fmt.Errorf("读取本地文件失败: %w", err)
	}

	// OSS SDK 的 UploadFile 不直接接收 context，通过取消标记在分片循环中尽早退出。
	if err := ctx.Err(); err != nil {
		return "", err
	}

	err := p.bucket.UploadFile(objectKey, localPath, p.opts.ossPartSizeBytes(), p.uploadOptions()...)
	if err != nil {
		return "", fmt.Errorf("OSS 分片上传失败: %w", err)
	}
	return p.accessURL(objectKey)
}

// UploadReader 将数据流上传到 OSS；已知大小时走分片上传，否则退化为单次 PutObject。
func (p *ossProvider) UploadReader(ctx context.Context, r io.Reader, objectKey string, size int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// 数据量较大时先落盘，复用分片上传的断点续传能力。
	if size >= p.opts.ossPartSizeBytes() {
		tmpFile, err := os.CreateTemp(p.opts.CheckpointDir, "storage-oss-upload-*")
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

	err := p.bucket.PutObject(objectKey, r)
	if err != nil {
		return "", fmt.Errorf("OSS 上传失败: %w", err)
	}
	return p.accessURL(objectKey)
}

// ossCheckpointDir 返回 OSS 断点续传目录，默认使用系统临时目录下的子目录。
func ossCheckpointDir(opts UploadOptions) string {
	if opts.CheckpointDir != "" {
		return filepath.Join(opts.CheckpointDir, "oss-checkpoint")
	}
	return filepath.Join(os.TempDir(), "storage-oss-checkpoint")
}
