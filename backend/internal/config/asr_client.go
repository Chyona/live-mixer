package config

import (
	"time"

	"live-mixer/internal/pkg/asr"
)

// ASRClientConfig 将应用 ASR 配置转换为豆包 ASR 客户端配置。
func (c ASRConfig) ASRClientConfig() asr.Config {
	return asr.Config{
		APIKey:       c.APIKey,
		BaseURL:      c.BaseURL,
		ResourceID:   c.ResourceID,
		PollInterval: time.Duration(c.PollIntervalSec) * time.Second,
		MaxPolls:     c.MaxPolls,
	}
}
