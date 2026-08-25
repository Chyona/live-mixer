package bootstrap

import (
	"go.uber.org/zap"
)

// RedisClient Redis 客户端占位类型。
// 当前项目不使用 Redis，保留此文件以符合框架结构，后续可按需扩展。
type RedisClient struct{}

// InitRedis 初始化 Redis 连接（当前未启用，直接返回 nil）。
func InitRedis(_ *zap.Logger) (*RedisClient, error) {
	// 项目不使用 Redis，跳过连接初始化
	return nil, nil
}
