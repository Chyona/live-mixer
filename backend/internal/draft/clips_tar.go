package draft

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"live-mixer/internal/draft/steps"
	"live-mixer/internal/pkg/storage"
)

// BuildDraftClipsTarObjectKey 生成切片 tar 包对象键：temp/draft/{jobID}/{jobID}.tar。
func BuildDraftClipsTarObjectKey(jobID string) string {
	id := strings.TrimSpace(jobID)
	if id == "" {
		id = "unknown"
	}
	return path.Join(storage.SubDirTemp, "draft", id, id+".tar")
}

// PackClipsTar 使用系统 tar 将本地切片打包为 {jobID}.tar，返回本地 tar 路径。
// clipPaths 为空时返回空路径与 nil error（跳过打包）。
func PackClipsTar(ctx context.Context, stagingDir, jobID string, clipPaths []string) (string, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return "", fmt.Errorf("jobID 不能为空")
	}
	stagingDir = strings.TrimSpace(stagingDir)
	if stagingDir == "" {
		return "", fmt.Errorf("stagingDir 不能为空")
	}

	names := make([]string, 0, len(clipPaths))
	for _, p := range clipPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("切片不存在 %s: %w", p, err)
		}
		names = append(names, filepath.Base(p))
	}
	if len(names) == 0 {
		return "", nil
	}

	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", fmt.Errorf("创建 staging 目录失败: %w", err)
	}
	tarPath := filepath.Join(stagingDir, jobID+".tar")
	// tar -cf <archive> -C <dir> file1 file2 ...
	args := append([]string{"-cf", tarPath, "-C", stagingDir}, names...)
	cmd := exec.CommandContext(ctx, "tar", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tar 打包失败: %w; output=%s", err, strings.TrimSpace(string(out)))
	}
	if st, err := os.Stat(tarPath); err != nil || st.Size() == 0 {
		return "", fmt.Errorf("tar 包未生成或为空: %s", tarPath)
	}
	return tarPath, nil
}

// PackAndUploadClipsTar 打包切片并上传对象存储，返回公网下载 URL。
// 无切片时返回空 URL；uploader 为空时返回错误。
func PackAndUploadClipsTar(ctx context.Context, uploader steps.ObjectUploader, stagingDir, jobID string, clipPaths []string) (string, error) {
	if uploader == nil {
		return "", fmt.Errorf("对象存储未配置，无法上传切片 tar 包")
	}
	tarPath, err := PackClipsTar(ctx, stagingDir, jobID, clipPaths)
	if err != nil {
		return "", err
	}
	if tarPath == "" {
		return "", nil
	}
	objectKey := BuildDraftClipsTarObjectKey(jobID)
	url, err := uploader.UploadFile(ctx, tarPath, objectKey)
	if err != nil {
		return "", fmt.Errorf("上传切片 tar 包失败: %w", err)
	}
	if strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("切片 tar 包上传后 URL 为空")
	}
	return url, nil
}
