// Package storage 提供多对象存储（Multi-Object-Storage）上传能力，
// 同时支持腾讯云 COS 与阿里云 OSS，并在两者均配置时优先使用 COS。
//
// 配置请通过 internal/config 加载后调用 NewClientFromAppConfig。
package storage
