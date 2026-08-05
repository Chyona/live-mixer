// Package config 负责加载应用配置，支持 embed 内嵌配置与外部文件路径。
package config

import (
	"embed"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

//go:embed config.yaml
var embeddedConfig embed.FS

// Config 应用全局配置结构体，字段与 config.yaml 一一对应。
type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Logger     LoggerConfig     `mapstructure:"logger"`
	JWT        JWTConfig        `mapstructure:"jwt"`
	Storage    StorageConfig    `mapstructure:"storage"`
	ASR        ASRConfig        `mapstructure:"asr"`
	LLM        LLMConfig        `mapstructure:"llm"`
	CapCutMate CapCutMateConfig `mapstructure:"capcut_mate"`
	Web        WebConfig        `mapstructure:"web"`
	Worker     WorkerConfig     `mapstructure:"worker"`
	Download   DownloadConfig   `mapstructure:"download"`
}

// WorkerConfig 后台任务 Worker 并发等调度配置。
type WorkerConfig struct {
	// AISliceConcurrency 单实例内并行执行 AI 切片任务的 Worker 数；默认 6。
	AISliceConcurrency int `mapstructure:"ai_slice_concurrency"`
	// ASRConcurrency 单实例内并行执行直播素材 ASR 的 Worker 数；默认 6。
	ASRConcurrency int `mapstructure:"asr_concurrency"`
	// DraftConcurrency 单实例内并行执行剪映草稿任务的 Worker 数；默认 3。
	DraftConcurrency int `mapstructure:"draft_concurrency"`
	// AISliceDraftConcurrency 单实例内并行执行一键成片任务的 Worker 数；默认 3。
	AISliceDraftConcurrency int `mapstructure:"ai_slice_draft_concurrency"`

	// ASRStaleTimeoutMin processing 孤儿回收阈值（分钟）：ASR 任务 asr_updated_at 超时未更新则改回 pending；默认 60。
	ASRStaleTimeoutMin int `mapstructure:"asr_stale_timeout_min"`
	// AISliceStaleTimeoutMin processing 孤儿回收阈值（分钟）：AI 切片任务 updated_at 超时未更新则改回 pending；默认 20。
	AISliceStaleTimeoutMin int `mapstructure:"ai_slice_stale_timeout_min"`
	// DraftStaleTimeoutMin processing 孤儿回收阈值（分钟）：草稿任务 updated_at 超时未更新则改回 pending；默认 60。
	DraftStaleTimeoutMin int `mapstructure:"draft_stale_timeout_min"`
	// AISliceDraftStaleTimeoutMin processing 孤儿回收阈值（分钟）：一键成片任务 updated_at 超时未更新则改回 pending；默认 90。
	AISliceDraftStaleTimeoutMin int `mapstructure:"ai_slice_draft_stale_timeout_min"`
}

// CapCutMateConfig 剪映草稿服务（capcut-mate）连接配置。
type CapCutMateConfig struct {
	// BaseURL capcut-mate REST API 根地址，默认 http://192.168.3.219:81
	BaseURL string `mapstructure:"base_url"`
	// APIKey 视频生成（gen_video）API 密钥；可用 APP_CAPCUT_MATE_API_KEY 覆盖。
	APIKey string `mapstructure:"api_key"`
	// GenVideoBaseURL 可选：gen_video / gen_video_status 共用的根地址；未配置时使用 BaseURL。
	// 可用 APP_CAPCUT_MATE_GEN_VIDEO_BASE_URL 覆盖。
	GenVideoBaseURL string `mapstructure:"gen_video_base_url"`
	// EnableGenVideo 是否在草稿成功后调用 gen_video；nil/未配置时默认 true。
	// 可用 APP_CAPCUT_MATE_ENABLE_GEN_VIDEO 覆盖（true/false、1/0、yes/no、on/off）。
	EnableGenVideo *bool `mapstructure:"enable_gen_video"`
	// GenVideoPollIntervalSec 视频生成状态轮询间隔（秒）；默认 5。
	GenVideoPollIntervalSec int `mapstructure:"gen_video_poll_interval_sec"`
	// GenVideoMaxPolls 视频生成最大轮询次数；默认 360（约 30 分钟）。
	GenVideoMaxPolls int `mapstructure:"gen_video_max_polls"`
}

// GenVideoEnabled 返回是否启用视频生成；未配置时默认启用。
func (c CapCutMateConfig) GenVideoEnabled() bool {
	if c.EnableGenVideo == nil {
		return true
	}
	return *c.EnableGenVideo
}

// WebConfig 本地暂存根目录：切片与 capcut-mate 请求记录、ASR 调试落盘路径。
// 切片对外 URL 由对象存储上传提供，不再需要 root_url。
type WebConfig struct {
	// RootDir 本地暂存根目录（切片落盘根路径），例如 D:\code\GitHub\live-mixer\docker\html
	RootDir string `mapstructure:"root_dir"`
	// StagingMaxDirs staging 下最多保留的任务子目录数（按 mtime 保留最新）；超出删除最旧；默认 80。
	StagingMaxDirs int `mapstructure:"staging_max_dirs"`
	// ASRStagingMaxDirs staging/asr 下最多保留的 ASR 调试子目录数（按 mtime 保留最新）；默认 20。
	ASRStagingMaxDirs int `mapstructure:"asr_staging_max_dirs"`
	// StagingCleanupIntervalMin staging 清理任务执行间隔（分钟）；默认 60。
	StagingCleanupIntervalMin int `mapstructure:"staging_cleanup_interval_min"`
}

// ServerConfig HTTP 服务相关配置。
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
}

// DatabaseConfig PostgreSQL 数据库连接配置。
type DatabaseConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	DBName       string `mapstructure:"dbname"`
	SSLMode      string `mapstructure:"sslmode"`
	Timezone     string `mapstructure:"timezone"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
}

// LoggerConfig 日志输出配置。
type LoggerConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	Filename   string `mapstructure:"filename"`
	MaxSize    int    `mapstructure:"max_size"`    // 单个日志文件最大体积（MB）
	MaxBackups int    `mapstructure:"max_backups"` // 最多保留的历史日志文件数量
}

// JWTConfig JWT 鉴权配置。
type JWTConfig struct {
	Secret    string `mapstructure:"secret"`
	ExpiresIn int    `mapstructure:"expires_in"` // 过期时间（秒）
}

// StorageConfig 对象存储配置，多个后端同时配置完整时优先级为 COS > OSS > TOS。
type StorageConfig struct {
	BasePath            string           `mapstructure:"base_path"`
	SignedURLExpireDays int              `mapstructure:"signed_url_expire_days"`
	COS                 COSStorageConfig `mapstructure:"cos"`
	OSS      OSSStorageConfig `mapstructure:"oss"`
	TOS      TOSStorageConfig `mapstructure:"tos"`
}

// COSStorageConfig 腾讯云对象存储（COS）连接配置。
type COSStorageConfig struct {
	SecretID   string `mapstructure:"secret_id"`
	SecretKey  string `mapstructure:"secret_key"`
	BucketName string `mapstructure:"bucket_name"`
	Region     string `mapstructure:"region"`
}

// OSSStorageConfig 阿里云对象存储（OSS）连接配置。
type OSSStorageConfig struct {
	AccessKeyID     string `mapstructure:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret"`
	BucketName      string `mapstructure:"bucket_name"`
	Endpoint        string `mapstructure:"endpoint"`
}

// ASRConfig 豆包语音识别（ASR）配置。
type ASRConfig struct {
	APIKey          string `mapstructure:"api_key"`
	BaseURL         string `mapstructure:"base_url"`
	ResourceID      string `mapstructure:"resource_id"`
	PollIntervalSec int    `mapstructure:"poll_interval_sec"`
	MaxPolls        int    `mapstructure:"max_polls"`
}

// LLMConfig OpenAI 兼容协议大模型配置。
// Model 用于 AI 切片等；FlashModel 用于添加视频后的 ASR 后处理（asr_summaries / asr_paragraphs）。
type LLMConfig struct {
	APIKey     string `mapstructure:"api_key"`
	BaseURL    string `mapstructure:"base_url"`
	Model      string `mapstructure:"model"`
	FlashModel string `mapstructure:"flash_model"`
}

// TOSStorageConfig 火山引擎对象存储（TOS）连接配置。
type TOSStorageConfig struct {
	AccessKeyID     string `mapstructure:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret"`
	BucketName      string `mapstructure:"bucket_name"`
	Region          string `mapstructure:"region"`
	Endpoint        string `mapstructure:"endpoint"`
}

// DSN 生成 PostgreSQL 连接字符串。
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode, d.Timezone,
	)
}

// Addr 返回 HTTP 监听地址。
func (s ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// Load 加载配置。
// 优先级：环境变量 APP_* > 外部配置文件（-config）> 内嵌 config.yaml。
func Load(configPath string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("读取外部配置文件失败: %w", err)
		}
	} else {
		v.SetConfigType("yaml")
		data, err := embeddedConfig.ReadFile("config.yaml")
		if err != nil {
			return nil, fmt.Errorf("读取内嵌配置文件失败: %w", err)
		}
		if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
			return nil, fmt.Errorf("解析内嵌配置文件失败: %w", err)
		}
	}

	// APP_DOWNLOAD_HOST_MAPPINGS 为自定义 from->to 规则字符串，须避免 viper AutomaticEnv 按 slice 解析失败。
	downloadMappingsEnv, hasDownloadMappings := os.LookupEnv("APP_DOWNLOAD_HOST_MAPPINGS")
	if hasDownloadMappings {
		os.Unsetenv("APP_DOWNLOAD_HOST_MAPPINGS")
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("反序列化配置失败: %w", err)
	}

	if hasDownloadMappings {
		os.Setenv("APP_DOWNLOAD_HOST_MAPPINGS", downloadMappingsEnv)
	}

	applyEnvOverrides(&cfg)
	normalizeWorkerConfig(&cfg.Worker)
	normalizeWebConfig(&cfg.Web)
	return &cfg, nil
}

// DefaultAISliceConcurrency AI 切片 Worker 默认并发数。
const DefaultAISliceConcurrency = 6

// DefaultASRConcurrency 直播素材 ASR Worker 默认并发数。
const DefaultASRConcurrency = 6

// DefaultDraftConcurrency 剪映草稿 Worker 默认并发数。
const DefaultDraftConcurrency = 3

// DefaultAISliceDraftConcurrency 一键成片 Worker 默认并发数。
const DefaultAISliceDraftConcurrency = 3

// DefaultASRStaleTimeoutMin ASR processing 孤儿回收默认超时（分钟）。
const DefaultASRStaleTimeoutMin = 60

// DefaultAISliceStaleTimeoutMin AI 切片 processing 孤儿回收默认超时（分钟）。
const DefaultAISliceStaleTimeoutMin = 20

// DefaultDraftStaleTimeoutMin 草稿生成 processing 孤儿回收默认超时（分钟）。
const DefaultDraftStaleTimeoutMin = 60

// DefaultAISliceDraftStaleTimeoutMin 一键成片 processing 孤儿回收默认超时（分钟）。
const DefaultAISliceDraftStaleTimeoutMin = 90

// DefaultStagingMaxDirs staging 下默认最多保留的任务子目录数。
const DefaultStagingMaxDirs = 80

// DefaultASRStagingMaxDirs staging/asr 下默认最多保留的 ASR 调试子目录数。
const DefaultASRStagingMaxDirs = 20

// DefaultStagingCleanupIntervalMin staging 清理任务默认执行间隔（分钟）。
const DefaultStagingCleanupIntervalMin = 60

// normalizeWorkerConfig 将未配置或非法的 Worker 并发/超时回落到内置默认值。
func normalizeWorkerConfig(w *WorkerConfig) {
	if w.AISliceConcurrency <= 0 {
		w.AISliceConcurrency = DefaultAISliceConcurrency
	}
	if w.ASRConcurrency <= 0 {
		w.ASRConcurrency = DefaultASRConcurrency
	}
	if w.DraftConcurrency <= 0 {
		w.DraftConcurrency = DefaultDraftConcurrency
	}
	if w.AISliceDraftConcurrency <= 0 {
		w.AISliceDraftConcurrency = DefaultAISliceDraftConcurrency
	}
	if w.ASRStaleTimeoutMin <= 0 {
		w.ASRStaleTimeoutMin = DefaultASRStaleTimeoutMin
	}
	if w.AISliceStaleTimeoutMin <= 0 {
		w.AISliceStaleTimeoutMin = DefaultAISliceStaleTimeoutMin
	}
	if w.DraftStaleTimeoutMin <= 0 {
		w.DraftStaleTimeoutMin = DefaultDraftStaleTimeoutMin
	}
	if w.AISliceDraftStaleTimeoutMin <= 0 {
		w.AISliceDraftStaleTimeoutMin = DefaultAISliceDraftStaleTimeoutMin
	}
}

// AISliceConcurrencyOrDefault 返回可用的 AI 切片并发（<=0 时回落默认 6）。
func (w WorkerConfig) AISliceConcurrencyOrDefault() int {
	if w.AISliceConcurrency <= 0 {
		return DefaultAISliceConcurrency
	}
	return w.AISliceConcurrency
}

// ASRConcurrencyOrDefault 返回可用的 ASR 并发（<=0 时回落默认 6）。
func (w WorkerConfig) ASRConcurrencyOrDefault() int {
	if w.ASRConcurrency <= 0 {
		return DefaultASRConcurrency
	}
	return w.ASRConcurrency
}

// DraftConcurrencyOrDefault 返回可用的草稿并发（<=0 时回落默认 3）。
func (w WorkerConfig) DraftConcurrencyOrDefault() int {
	if w.DraftConcurrency <= 0 {
		return DefaultDraftConcurrency
	}
	return w.DraftConcurrency
}

// AISliceDraftConcurrencyOrDefault 返回可用的一键成片并发（<=0 时回落默认 3）。
func (w WorkerConfig) AISliceDraftConcurrencyOrDefault() int {
	if w.AISliceDraftConcurrency <= 0 {
		return DefaultAISliceDraftConcurrency
	}
	return w.AISliceDraftConcurrency
}

// ASRStaleTimeout 返回 ASR 孤儿回收超时；<=0 时回落默认 60 分钟。
func (w WorkerConfig) ASRStaleTimeout() time.Duration {
	min := w.ASRStaleTimeoutMin
	if min <= 0 {
		min = DefaultASRStaleTimeoutMin
	}
	return time.Duration(min) * time.Minute
}

// AISliceStaleTimeout 返回 AI 切片孤儿回收超时；<=0 时回落默认 20 分钟。
func (w WorkerConfig) AISliceStaleTimeout() time.Duration {
	min := w.AISliceStaleTimeoutMin
	if min <= 0 {
		min = DefaultAISliceStaleTimeoutMin
	}
	return time.Duration(min) * time.Minute
}

// DraftStaleTimeout 返回草稿生成孤儿回收超时；<=0 时回落默认 60 分钟。
func (w WorkerConfig) DraftStaleTimeout() time.Duration {
	min := w.DraftStaleTimeoutMin
	if min <= 0 {
		min = DefaultDraftStaleTimeoutMin
	}
	return time.Duration(min) * time.Minute
}

// AISliceDraftStaleTimeout 返回一键成片孤儿回收超时；<=0 时回落默认 90 分钟。
func (w WorkerConfig) AISliceDraftStaleTimeout() time.Duration {
	min := w.AISliceDraftStaleTimeoutMin
	if min <= 0 {
		min = DefaultAISliceDraftStaleTimeoutMin
	}
	return time.Duration(min) * time.Minute
}

// normalizeWebConfig 将未配置或非法的 staging 清理参数回落到内置默认值。
func normalizeWebConfig(w *WebConfig) {
	if w.StagingMaxDirs <= 0 {
		w.StagingMaxDirs = DefaultStagingMaxDirs
	}
	if w.ASRStagingMaxDirs <= 0 {
		w.ASRStagingMaxDirs = DefaultASRStagingMaxDirs
	}
	if w.StagingCleanupIntervalMin <= 0 {
		w.StagingCleanupIntervalMin = DefaultStagingCleanupIntervalMin
	}
}

// StagingCleanupInterval 返回 staging 清理间隔；<=0 时回落默认 60 分钟。
func (w WebConfig) StagingCleanupInterval() time.Duration {
	min := w.StagingCleanupIntervalMin
	if min <= 0 {
		min = DefaultStagingCleanupIntervalMin
	}
	return time.Duration(min) * time.Minute
}

// applyEnvOverrides 用已设置的环境变量 APP_* 覆盖配置（仅当环境变量存在时生效）。
func applyEnvOverrides(cfg *Config) {
	if val, ok := os.LookupEnv("APP_SERVER_HOST"); ok {
		cfg.Server.Host = val
	}
	if val, ok := os.LookupEnv("APP_SERVER_PORT"); ok {
		if port, err := strconv.Atoi(val); err == nil {
			cfg.Server.Port = port
		}
	}
	if val, ok := os.LookupEnv("APP_SERVER_MODE"); ok {
		cfg.Server.Mode = val
	}

	if val, ok := os.LookupEnv("APP_DATABASE_HOST"); ok {
		cfg.Database.Host = val
	}
	if val, ok := os.LookupEnv("APP_DATABASE_PORT"); ok {
		if port, err := strconv.Atoi(val); err == nil {
			cfg.Database.Port = port
		}
	}
	if val, ok := os.LookupEnv("APP_DATABASE_USER"); ok {
		cfg.Database.User = val
	}
	if val, ok := os.LookupEnv("APP_DATABASE_PASSWORD"); ok {
		cfg.Database.Password = val
	}
	if val, ok := os.LookupEnv("APP_DATABASE_DBNAME"); ok {
		cfg.Database.DBName = val
	}
	if val, ok := os.LookupEnv("APP_DATABASE_SSLMODE"); ok {
		cfg.Database.SSLMode = val
	}
	if val, ok := os.LookupEnv("APP_DATABASE_TIMEZONE"); ok {
		cfg.Database.Timezone = val
	}
	if val, ok := os.LookupEnv("APP_DATABASE_MAX_IDLE_CONNS"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Database.MaxIdleConns = n
		}
	}
	if val, ok := os.LookupEnv("APP_DATABASE_MAX_OPEN_CONNS"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Database.MaxOpenConns = n
		}
	}

	if val, ok := os.LookupEnv("APP_LOGGER_LEVEL"); ok {
		cfg.Logger.Level = val
	}
	if val, ok := os.LookupEnv("APP_LOGGER_FORMAT"); ok {
		cfg.Logger.Format = val
	}
	if val, ok := os.LookupEnv("APP_LOGGER_FILENAME"); ok {
		cfg.Logger.Filename = val
	}
	if val, ok := os.LookupEnv("APP_LOGGER_MAX_SIZE"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Logger.MaxSize = n
		}
	}
	if val, ok := os.LookupEnv("APP_LOGGER_MAX_BACKUPS"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Logger.MaxBackups = n
		}
	}

	if val, ok := os.LookupEnv("APP_JWT_SECRET"); ok {
		cfg.JWT.Secret = val
	}
	if val, ok := os.LookupEnv("APP_JWT_EXPIRES_IN"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.JWT.ExpiresIn = n
		}
	}

	if val, ok := os.LookupEnv("APP_STORAGE_COS_SECRET_ID"); ok {
		cfg.Storage.COS.SecretID = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_COS_SECRET_KEY"); ok {
		cfg.Storage.COS.SecretKey = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_COS_BUCKET_NAME"); ok {
		cfg.Storage.COS.BucketName = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_COS_REGION"); ok {
		cfg.Storage.COS.Region = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_OSS_ACCESS_KEY_ID"); ok {
		cfg.Storage.OSS.AccessKeyID = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_OSS_ACCESS_KEY_SECRET"); ok {
		cfg.Storage.OSS.AccessKeySecret = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_OSS_BUCKET_NAME"); ok {
		cfg.Storage.OSS.BucketName = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_OSS_ENDPOINT"); ok {
		cfg.Storage.OSS.Endpoint = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_TOS_ACCESS_KEY_ID"); ok {
		cfg.Storage.TOS.AccessKeyID = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_TOS_ACCESS_KEY_SECRET"); ok {
		cfg.Storage.TOS.AccessKeySecret = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_TOS_BUCKET_NAME"); ok {
		cfg.Storage.TOS.BucketName = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_TOS_REGION"); ok {
		cfg.Storage.TOS.Region = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_TOS_ENDPOINT"); ok {
		cfg.Storage.TOS.Endpoint = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_BASE_PATH"); ok {
		cfg.Storage.BasePath = val
	}
	if val, ok := os.LookupEnv("APP_STORAGE_SIGNED_URL_EXPIRE_DAYS"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Storage.SignedURLExpireDays = n
		}
	}

	if val, ok := os.LookupEnv("APP_ASR_API_KEY"); ok {
		cfg.ASR.APIKey = val
	}
	if val, ok := os.LookupEnv("APP_ASR_BASE_URL"); ok {
		cfg.ASR.BaseURL = val
	}
	if val, ok := os.LookupEnv("APP_ASR_RESOURCE_ID"); ok {
		cfg.ASR.ResourceID = val
	}
	if val, ok := os.LookupEnv("APP_ASR_POLL_INTERVAL_SEC"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.ASR.PollIntervalSec = n
		}
	}
	if val, ok := os.LookupEnv("APP_ASR_MAX_POLLS"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.ASR.MaxPolls = n
		}
	}

	if val, ok := os.LookupEnv("APP_LLM_API_KEY"); ok {
		cfg.LLM.APIKey = val
	}
	if val, ok := os.LookupEnv("APP_LLM_BASE_URL"); ok {
		cfg.LLM.BaseURL = val
	}
	if val, ok := os.LookupEnv("APP_LLM_MODEL"); ok {
		cfg.LLM.Model = val
	}
	if val, ok := os.LookupEnv("APP_LLM_FLASH_MODEL"); ok {
		cfg.LLM.FlashModel = val
	}

	if val, ok := os.LookupEnv("APP_WORKER_AI_SLICE_CONCURRENCY"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Worker.AISliceConcurrency = n
		}
	}
	if val, ok := os.LookupEnv("APP_WORKER_ASR_CONCURRENCY"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Worker.ASRConcurrency = n
		}
	}
	if val, ok := os.LookupEnv("APP_WORKER_DRAFT_CONCURRENCY"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Worker.DraftConcurrency = n
		}
	}
	if val, ok := os.LookupEnv("APP_WORKER_AI_SLICE_DRAFT_CONCURRENCY"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Worker.AISliceDraftConcurrency = n
		}
	}
	if val, ok := os.LookupEnv("APP_WORKER_ASR_STALE_TIMEOUT_MIN"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Worker.ASRStaleTimeoutMin = n
		}
	}
	if val, ok := os.LookupEnv("APP_WORKER_AI_SLICE_STALE_TIMEOUT_MIN"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Worker.AISliceStaleTimeoutMin = n
		}
	}
	if val, ok := os.LookupEnv("APP_WORKER_DRAFT_STALE_TIMEOUT_MIN"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Worker.DraftStaleTimeoutMin = n
		}
	}
	if val, ok := os.LookupEnv("APP_WORKER_AI_SLICE_DRAFT_STALE_TIMEOUT_MIN"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Worker.AISliceDraftStaleTimeoutMin = n
		}
	}

	// 剪映草稿服务与本地暂存目录（也可兼容无 APP_ 前缀的环境变量名）
	if val, ok := lookupEnvPrefer("APP_CAPCUT_MATE_BASE_URL", "CAPCUT_MATE_URL"); ok {
		cfg.CapCutMate.BaseURL = val
	}
	if val, ok := lookupEnvPrefer("APP_CAPCUT_MATE_API_KEY", "CAPCUT_MATE_API_KEY"); ok {
		cfg.CapCutMate.APIKey = val
	}
	if val, ok := os.LookupEnv("APP_CAPCUT_MATE_GEN_VIDEO_BASE_URL"); ok {
		cfg.CapCutMate.GenVideoBaseURL = val
	}
	if val, ok := os.LookupEnv("APP_CAPCUT_MATE_ENABLE_GEN_VIDEO"); ok {
		if b, err := parseEnvBool(val); err == nil {
			cfg.CapCutMate.EnableGenVideo = &b
		}
	}
	if val, ok := os.LookupEnv("APP_CAPCUT_MATE_GEN_VIDEO_POLL_INTERVAL_SEC"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.CapCutMate.GenVideoPollIntervalSec = n
		}
	}
	if val, ok := os.LookupEnv("APP_CAPCUT_MATE_GEN_VIDEO_MAX_POLLS"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.CapCutMate.GenVideoMaxPolls = n
		}
	}
	if val, ok := lookupEnvPrefer("APP_WEB_ROOT_DIR", "WEB_ROOT_DIR"); ok {
		cfg.Web.RootDir = val
	}
	if val, ok := os.LookupEnv("APP_WEB_STAGING_MAX_DIRS"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Web.StagingMaxDirs = n
		}
	}
	if val, ok := os.LookupEnv("APP_WEB_ASR_STAGING_MAX_DIRS"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Web.ASRStagingMaxDirs = n
		}
	}
	if val, ok := os.LookupEnv("APP_WEB_STAGING_CLEANUP_INTERVAL_MIN"); ok {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Web.StagingCleanupIntervalMin = n
		}
	}

	if val, ok := os.LookupEnv("APP_DOWNLOAD_HOST_MAPPINGS"); ok {
		cfg.Download.HostMappings = hostMappingsFromEnv(val)
	}
}

func hostMappingsFromEnv(raw string) []HostMapping {
	rules := parseRewriteRules(raw)
	out := make([]HostMapping, 0, len(rules))
	for _, rule := range rules {
		out = append(out, HostMapping{From: rule[0], To: rule[1]})
	}
	return out
}

// lookupEnvPrefer 依次查找多个环境变量名，返回第一个已设置的值。
func lookupEnvPrefer(keys ...string) (string, bool) {
	for _, key := range keys {
		if val, ok := os.LookupEnv(key); ok {
			return val, true
		}
	}
	return "", false
}

// parseEnvBool 解析常见布尔环境变量写法（true/false、1/0、yes/no、on/off）。
func parseEnvBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool %q", raw)
	}
}
