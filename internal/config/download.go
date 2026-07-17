package config

import (
	"strings"

	"live-mixer/pkg/utils/urlrewrite"
)

// HostMapping 下载时按完整 Host 精确替换。
type HostMapping struct {
	From string `mapstructure:"from"`
	To   string `mapstructure:"to"`
}

// DownloadConfig 远程文件下载相关配置。
type DownloadConfig struct {
	HostMappings []HostMapping `mapstructure:"host_mappings"`
}

// URLRewriter 根据配置构建 URL Host 改写器；无规则时返回 Empty 实例。
func (d DownloadConfig) URLRewriter() *urlrewrite.Rewriter {
	exact := make(map[string]string, len(d.HostMappings))
	for _, item := range d.HostMappings {
		from := strings.TrimSpace(item.From)
		to := strings.TrimSpace(item.To)
		if from == "" || to == "" {
			continue
		}
		exact[from] = to
	}

	return urlrewrite.New(urlrewrite.Options{Exact: exact})
}

// parseRewriteRules 解析环境变量中的改写规则，格式：from->to,from2->to2。
func parseRewriteRules(raw string) [][2]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var rules [][2]string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		from, to, ok := strings.Cut(part, "->")
		from = strings.TrimSpace(from)
		to = strings.TrimSpace(to)
		if !ok || from == "" || to == "" {
			continue
		}
		rules = append(rules, [2]string{from, to})
	}
	return rules
}
