package service

import (
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"live-mixer/internal/pkg/storage"
)

const asrTempFileExt = ".mp4"

// newASRSessionID 生成 ASR 临时文件会话 ID，便于本地下载与对象存储键名共用同一 uuid。
var newASRSessionID = func() string {
	return uuid.NewString()
}

// buildASRLocalFileName 生成本地临时文件名，例如 asr_550e8400-e29b-41d4-a716-446655440000.mp4。
func buildASRLocalFileName(sessionID string) string {
	return "asr_" + sessionID + asrTempFileExt
}

// buildASRLocalPath 生成本地临时文件完整路径，例如 temp/asr_{uuid}.mp4。
func buildASRLocalPath(tempDir, sessionID string) string {
	return filepath.Join(tempDir, buildASRLocalFileName(sessionID))
}

// buildASRObjectKey 生成对象存储相对键名，例如 temp/asr_{uuid}.mp4（由客户端附加 base_path）。
func buildASRObjectKey(prefix, sessionID string) string {
	return joinASRStorageKey(prefix, sessionID, asrTempFileExt)
}

func joinASRStorageKey(prefix, sessionID, ext string) string {
	prefix = strings.Trim(prefix, "/")
	fileName := "asr_" + sessionID + ext
	if prefix == "" {
		return fileName
	}
	return storage.JoinObjectKey(prefix, fileName)
}
