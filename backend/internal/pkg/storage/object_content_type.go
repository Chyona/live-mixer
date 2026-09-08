package storage

import (
	"path"
	"strings"
)

// objectContentType 按对象键后缀返回上传 Content-Type；未知类型返回空串，交给存储后端默认推断。
func objectContentType(objectKey string) string {
	switch strings.ToLower(path.Ext(objectKey)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/MP2T"
	default:
		return ""
	}
}
