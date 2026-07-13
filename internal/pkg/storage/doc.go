// Package storage 提供多对象存储（Multi-Object-Storage）上传能力，
// 同时支持腾讯云 COS、阿里云 OSS 与火山引擎 TOS，
// 多个后端均配置时优先级为 COS > OSS > TOS。
//
// 上传时自动将相对对象键附加 base_path 前缀（默认 video_editing）。
// 约定子目录（相对 base_path）：
//   - temp/  临时文件（如 ASR 前上传获取公网 URL）
//   - test/  cmd/test 进程产生的测试上传
//
// 配置请通过 internal/config 加载后调用 NewClientFromAppConfig。
package storage
