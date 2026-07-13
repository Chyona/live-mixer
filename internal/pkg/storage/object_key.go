package storage

import "strings"

const (
	// DefaultBasePath 对象键默认前缀，上传文件保存在桶内该目录下。
	DefaultBasePath = "video_editing"

	// SubDirTemp 临时文件目录（相对 base_path），如 ASR 前上传获取公网 URL。
	SubDirTemp = "temp"
	// SubDirTest cmd/test 进程产生的测试上传目录（相对 base_path）；单元测试不受此约定约束。
	SubDirTest = "test"
)

// ResolveBasePath 规范化保存路径；空值时返回 DefaultBasePath。
func ResolveBasePath(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return DefaultBasePath
	}
	return path
}

// JoinObjectKey 将保存路径前缀与相对对象键拼接为完整对象键。
func JoinObjectKey(basePath, objectKey string) string {
	basePath = strings.Trim(basePath, "/")
	objectKey = strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	if basePath == "" {
		return objectKey
	}
	if objectKey == "" {
		return basePath
	}
	return basePath + "/" + objectKey
}
