// Package urlrewrite 在下载等出站请求前将 URL Host 精确替换为内网域名，path 与 query 保持不变。
package urlrewrite

import (
	"net"
	"net/url"
	"strings"
)

// Options 构建 Rewriter 的 Host 精确映射规则。
type Options struct {
	Exact map[string]string
}

// Rewriter 根据配置改写 URL Host。
type Rewriter struct {
	exact map[string]string
}

// New 根据规则创建 Rewriter；无有效规则时返回 Empty 的实例。
func New(opts Options) *Rewriter {
	exact := make(map[string]string, len(opts.Exact))
	for k, v := range opts.Exact {
		from := normalizeHost(k)
		to := normalizeHost(v)
		if from == "" || to == "" {
			continue
		}
		exact[from] = to
	}

	if len(exact) == 0 {
		return &Rewriter{}
	}
	return &Rewriter{exact: exact}
}

// Empty 表示未配置任何改写规则。
func (r *Rewriter) Empty() bool {
	return r == nil || len(r.exact) == 0
}

// Rewrite 尝试改写 URL；第二个返回值为 true 表示 Host 已替换。
func (r *Rewriter) Rewrite(raw string) (string, bool) {
	if r == nil || r.Empty() {
		return raw, false
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw, false
	}

	hostname := hostnameOnly(u.Host)
	mapped, ok := r.exact[hostname]
	if !ok {
		return raw, false
	}

	u.Host = replaceHostname(u.Host, mapped)
	return u.String(), true
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}
	return hostnameOnly(host)
}

func hostnameOnly(host string) string {
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	return hostname
}

func replaceHostname(host, newHostname string) string {
	_, port, err := net.SplitHostPort(host)
	if err != nil {
		return newHostname
	}
	return net.JoinHostPort(newHostname, port)
}
