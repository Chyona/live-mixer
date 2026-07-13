package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// cosProvider 腾讯云 COS 存储后端实现。
type cosProvider struct {
	client              *cos.Client
	bucketName          string
	region              string
	opts                UploadOptions
	signedURLExpireDays int
}

// newCOSProvider 创建 COS 存储后端。
func newCOSProvider(cfg COSConfig, opts UploadOptions, signedURLExpireDays int) (*cosProvider, error) {
	bucketURL, err := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.BucketName, cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("解析 COS Bucket URL 失败: %w", err)
	}

	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  cfg.SecretID,
			SecretKey: cfg.SecretKey,
		},
	})

	return &cosProvider{
		client:              client,
		bucketName:          cfg.BucketName,
		region:              cfg.Region,
		opts:                opts,
		signedURLExpireDays: signedURLExpireDays,
	}, nil
}

// newCOSProviderWithClient 使用自定义客户端创建 COS 后端，仅供单元测试注入 mock HTTP 服务。
func newCOSProviderWithClient(client *cos.Client, bucketName, region string, opts UploadOptions, signedURLExpireDays int) *cosProvider {
	return &cosProvider{
		client:              client,
		bucketName:          bucketName,
		region:              region,
		opts:                opts,
		signedURLExpireDays: signedURLExpireDays,
	}
}

func (p *cosProvider) Type() ProviderType {
	return ProviderCOS
}

// objectURL 拼接对象的公开访问地址（未签名）。
func (p *cosProvider) objectURL(objectKey string) string {
	return fmt.Sprintf("https://%s.cos.%s.myqcloud.com/%s", p.bucketName, p.region, objectKey)
}

// presignedURL 生成带有效期的 GET 签名链接。
func (p *cosProvider) presignedURL(ctx context.Context, objectKey string) (string, error) {
	expire := signedURLExpireDuration(p.signedURLExpireDays, ProviderCOS)
	u, err := p.client.Object.GetPresignedURL2(ctx, http.MethodGet, objectKey, expire, nil)
	if err != nil {
		return "", fmt.Errorf("COS 生成签名 URL 失败: %w", err)
	}
	return u.String(), nil
}

// accessURL 返回上传完成后的可访问链接（默认带签名）。
func (p *cosProvider) accessURL(ctx context.Context, objectKey string) (string, error) {
	return p.presignedURL(ctx, objectKey)
}

// UploadFile 使用 COS 分片上传本地文件，并启用断点续传以应对网络中断。
func (p *cosProvider) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	if _, err := os.Stat(localPath); err != nil {
		return "", fmt.Errorf("读取本地文件失败: %w", err)
	}

	uploadOpts := &cos.MultiUploadOptions{
		PartSize:       p.opts.cosPartSizeMB(),
		ThreadPoolSize: p.opts.concurrency(),
		CheckPoint:     !p.opts.DisableCheckpoint, // 启用断点续传，网络中断后可从已上传分片继续
	}

	_, _, err := p.client.Object.Upload(ctx, objectKey, localPath, uploadOpts)
	if err != nil {
		return "", fmt.Errorf("COS 分片上传失败: %w", err)
	}
	return p.accessURL(ctx, objectKey)
}

// UploadReader 将数据流写入临时文件后走分片上传，保证与 UploadFile 相同的弱网容错能力。
func (p *cosProvider) UploadReader(ctx context.Context, r io.Reader, objectKey string, size int64) (string, error) {
	tmpFile, err := os.CreateTemp(p.opts.CheckpointDir, "storage-cos-upload-*")
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
	if size >= 0 && written != size {
		return "", fmt.Errorf("上传数据大小不匹配: 期望 %d 字节，实际 %d 字节", size, written)
	}

	return p.UploadFile(ctx, tmpPath, objectKey)
}
