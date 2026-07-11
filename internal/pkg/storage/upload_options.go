package storage

const (
	// defaultPartSizeMB COS 分片大小默认值（单位：MB）。
	defaultPartSizeMB = 5
	// defaultPartSizeBytes OSS / TOS 分片大小默认值（单位：字节）。
	defaultPartSizeBytes = 5 * 1024 * 1024
	// defaultConcurrency 并发上传分片的协程数。
	defaultConcurrency = 3
)

// UploadOptions 控制分片上传行为，用于在弱网环境下提升上传成功率。
type UploadOptions struct {
	// PartSizeMB COS 分片大小（MB），小于等于 0 时使用默认值。
	PartSizeMB int
	// PartSizeBytes OSS / TOS 分片大小（字节），小于等于 0 时使用默认值。
	PartSizeBytes int64
	// Concurrency 并发上传分片数，小于等于 0 时使用默认值。
	Concurrency int
	// CheckpointDir 断点续传检查点目录，为空时写入系统临时目录。
	CheckpointDir string
	// DisableCheckpoint 禁用断点续传（通常仅用于单元测试）。
	DisableCheckpoint bool
}

func (o UploadOptions) cosPartSizeMB() int64 {
	if o.PartSizeMB > 0 {
		return int64(o.PartSizeMB)
	}
	return defaultPartSizeMB
}

func (o UploadOptions) ossPartSizeBytes() int64 {
	if o.PartSizeBytes > 0 {
		return o.PartSizeBytes
	}
	return defaultPartSizeBytes
}

// tosPartSizeBytes 返回 TOS 分片大小（字节），复用 PartSizeBytes 配置项。
func (o UploadOptions) tosPartSizeBytes() int64 {
	return o.ossPartSizeBytes()
}

func (o UploadOptions) concurrency() int {
	if o.Concurrency > 0 {
		return o.Concurrency
	}
	return defaultConcurrency
}
