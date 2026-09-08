package media

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const (
	hlsProbeTimeout     = 15 * time.Second
	hlsMaxPlaylistBytes = 2 << 20
	hlsMaxVariantDepth  = 3
)

// HLSProbeResult HLS 播放列表探测结果。
type HLSProbeResult struct {
	HasMedia     bool
	Encrypted    bool
	IsMaster     bool
	MediaURL     string
	VariantCount int
}

// ProbeHLSPlaylist 拉取 m3u8：检测加密、主列表与是否已有媒体分片。
// 有 EXTINF 视为已出流；EXT-X-KEY 视为加密（跟播应直接失败）。
func ProbeHLSPlaylist(client *http.Client, playlistURL string) (HLSProbeResult, error) {
	return probeHLSPlaylist(client, playlistURL, 0)
}

func probeHLSPlaylist(client *http.Client, playlistURL string, depth int) (HLSProbeResult, error) {
	empty := HLSProbeResult{}
	playlistURL = strings.TrimSpace(playlistURL)
	if playlistURL == "" {
		return empty, fmt.Errorf("m3u8 地址为空")
	}
	if depth > hlsMaxVariantDepth {
		return empty, fmt.Errorf("HLS 主播放列表嵌套过深")
	}
	if client == nil {
		client = &http.Client{Timeout: hlsProbeTimeout}
	}

	req, err := http.NewRequest(http.MethodGet, playlistURL, nil)
	if err != nil {
		return empty, fmt.Errorf("构造 HLS 请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "live-mixer-hls-probe/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return empty, fmt.Errorf("拉取 HLS 播放列表失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return empty, fmt.Errorf("HLS 播放列表 HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, hlsMaxPlaylistBytes+1))
	if err != nil {
		return empty, fmt.Errorf("读取 HLS 播放列表失败: %w", err)
	}
	if len(body) > hlsMaxPlaylistBytes {
		return empty, fmt.Errorf("HLS 播放列表过大")
	}
	text := string(body)
	if !strings.Contains(text, "#EXTM3U") {
		return empty, fmt.Errorf("不是有效的 HLS 播放列表")
	}

	result := HLSProbeResult{MediaURL: playlistURL}
	lines := strings.Split(text, "\n")
	var streamURLs []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "#EXT-X-KEY") {
			result.Encrypted = true
		}
		if strings.HasPrefix(upper, "#EXT-X-STREAM-INF") {
			result.IsMaster = true
		}
		if strings.HasPrefix(upper, "#EXTINF") {
			result.HasMedia = true
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if looksLikeHLSRef(line) {
			abs, absErr := resolvePlaylistRef(playlistURL, line)
			if absErr == nil {
				streamURLs = append(streamURLs, abs)
			}
		}
	}
	result.VariantCount = len(streamURLs)
	if result.Encrypted {
		return result, fmt.Errorf("不支持加密 HLS（EXT-X-KEY）")
	}
	if result.HasMedia {
		return result, nil
	}
	if !result.IsMaster || len(streamURLs) == 0 {
		return result, nil
	}
	// 主列表：探测码率最高的最后一条变体（通常分辨率更高）。
	child, err := probeHLSPlaylist(client, streamURLs[len(streamURLs)-1], depth+1)
	if err != nil && child.Encrypted {
		return child, err
	}
	if err != nil && !child.HasMedia {
		return result, nil
	}
	child.IsMaster = true
	child.VariantCount = len(streamURLs)
	if child.MediaURL == "" {
		child.MediaURL = streamURLs[len(streamURLs)-1]
	}
	return child, err
}

func looksLikeHLSRef(line string) bool {
	if strings.HasPrefix(line, "#") {
		return false
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, ".m3u8") {
		return true
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return strings.Contains(lower, ".m3u8")
	}
	return false
}

func resolvePlaylistRef(baseURL, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("空引用")
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref, nil
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(rel).String(), nil
}

// IsM3U8URL 路径或查询中含 .m3u8 视为 HLS。
func IsM3U8URL(raw string) bool {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return false
	}
	return strings.Contains(s, ".m3u8")
}

// IsHTTPURL 是否为 http(s) 远程地址。
func IsHTTPURL(raw string) bool {
	s := strings.TrimSpace(strings.ToLower(raw))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// SanitizeHLSError 去掉控制字符，避免把 ffmpeg 二进制输出写进数据库。
func SanitizeHLSError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 800 {
		s = s[:800]
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
