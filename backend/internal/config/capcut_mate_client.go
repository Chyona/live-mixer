package config

import (
	"time"

	"live-mixer/internal/pkg/capcutmate"
)

// CapCutMateClientConfig 将应用配置转为 capcut-mate 客户端配置。
func (c CapCutMateConfig) CapCutMateClientConfig() capcutmate.Config {
	cfg := capcutmate.Config{
		BaseURL:         c.BaseURL,
		APIKey:          c.APIKey,
		GenVideoBaseURL: c.GenVideoBaseURL,
		MaxPolls:        c.GenVideoMaxPolls,
	}
	if c.GenVideoPollIntervalSec > 0 {
		cfg.PollInterval = time.Duration(c.GenVideoPollIntervalSec) * time.Second
	}
	return cfg
}
