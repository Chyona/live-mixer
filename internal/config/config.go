// Package config 负责加载应用配置，支持 embed 内嵌配置与外部文件路径。
package config

import (
	"embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

//go:embed config.yaml
var embeddedConfig embed.FS

// Config 应用全局配置结构体，字段与 config.yaml 一一对应。
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Logger   LoggerConfig   `mapstructure:"logger"`
	JWT      JWTConfig      `mapstructure:"jwt"`
	Storage  StorageConfig  `mapstructure:"storage"`
	ASR      ASRConfig      `mapstructure:"asr"`
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
	COS COSStorageConfig `mapstructure:"cos"`
	OSS OSSStorageConfig `mapstructure:"oss"`
	TOS TOSStorageConfig `mapstructure:"tos"`
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

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("反序列化配置失败: %w", err)
	}

	applyEnvOverrides(&cfg)
	return &cfg, nil
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
}
