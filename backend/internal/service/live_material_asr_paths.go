package service

import (
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"live-mixer/internal/pkg/storage"
)

const (
	asrSourceFileExt = ".src" // 下载的原始媒体临时后缀
	asrTempFileExt   = ".mp3" // 转码后上传对象存储的标准 MP3
)

// newASRSessionID 生成 ASR 临时文件会话 ID，便于本地下载与对象存储键名共用同一 uuid。
var newASRSessionID = func() string {
	return uuid.NewString()
}

// buildASRSourceLocalFileName 生成本地下载临时文件名，例如 asr_{uuid}.src。
func buildASRSourceLocalFileName(sessionID string) string {
	return "asr_" + sessionID + asrSourceFileExt
}

// buildASRSourceLocalPath 生成本地下载临时文件完整路径。
func buildASRSourceLocalPath(tempDir, sessionID string) string {
	return filepath.Join(tempDir, buildASRSourceLocalFileName(sessionID))
}

// buildASRLocalFileName 生成本地 MP3 文件名，例如 asr_{uuid}.mp3。
func buildASRLocalFileName(sessionID string) string {
	return "asr_" + sessionID + asrTempFileExt
}

// buildASRLocalPath 生成本地 MP3 完整路径，例如 temp/asr_{uuid}.mp3。
func buildASRLocalPath(tempDir, sessionID string) string {
	return filepath.Join(tempDir, buildASRLocalFileName(sessionID))
}

// buildASRObjectKey 生成对象存储相对键名，例如 temp/asr_{uuid}.mp3（由客户端附加 base_path）。
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
