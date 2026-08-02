// Package asr 封装火山引擎豆包 BigModel ASR 同步调用能力。
package asr

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// 支持的音频/视频格式（创建素材校验与 ASR 链路识别后缀）；mov 经 ffmpeg 转码后送 ASR。
var supportedFormats = map[string]struct{}{
	"mp3": {},
	"wav": {},
	"mp4": {},
	"mov": {},
	"ogg": {},
	"raw": {},
}

// DetectFormat 从公网 URL 路径后缀推断媒体格式。
// 无法识别时默认返回 mp4（与 doubao-asr 参考脚本一致）。
func DetectFormat(audioURL string) (string, error) {
	if strings.TrimSpace(audioURL) == "" {
		return "", fmt.Errorf("audio_url 不能为空")
	}

	parsed, err := url.Parse(audioURL)
	if err != nil {
		return "", fmt.Errorf("audio_url 格式无效: %w", err)
	}

	ext := strings.TrimPrefix(strings.ToLower(path.Ext(parsed.Path)), ".")
	if ext == "" {
		return "mp4", nil
	}

	if _, ok := supportedFormats[ext]; !ok {
		return "", fmt.Errorf("不支持的音频格式 %q，支持: mp3, wav, mp4, mov, ogg, raw", ext)
	}
	return ext, nil
}
