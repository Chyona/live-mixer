package config

import "live-mixer/internal/pkg/capcutmate"

// CapCutMateClientConfig 将应用配置转为 capcut-mate 客户端配置。
func (c CapCutMateConfig) CapCutMateClientConfig() capcutmate.Config {
	return capcutmate.Config{BaseURL: c.BaseURL}
}
