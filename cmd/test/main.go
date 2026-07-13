// TOS 上传测试工具：使用内嵌 config.yaml（或 -config 指定外部配置）验证对象存储上传。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"live-mixer/internal/config"
	"live-mixer/internal/pkg/storage"
)

func main() {
	configPath := flag.String("config", "", "外部配置文件路径（默认使用内嵌 config.yaml）")
	localFile := flag.String("file", "", "待上传的本地文件（默认为本程序 main.go）")
	objectKey := flag.String("key", "", "对象键名（默认 video_editing/test/<文件名>）")
	flag.Parse()

	filePath := *localFile
	if filePath == "" {
		_, self, _, ok := runtime.Caller(0)
		if !ok {
			fmt.Fprintln(os.Stderr, "无法定位默认 main.go 路径，请使用 -file 指定")
			os.Exit(1)
		}
		filePath = self
	}

	if _, err := os.Stat(filePath); err != nil {
		fmt.Fprintf(os.Stderr, "本地文件不可用: %s: %v\n", filePath, err)
		os.Exit(1)
	}

	key := *objectKey
	if key == "" {
		key = filepath.ToSlash(filepath.Join("video_editing", "test", filepath.Base(filePath)))
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	client, err := storage.NewClientFromAppConfig(cfg.Storage)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建存储客户端失败: %v\n", err)
		os.Exit(1)
	}

	tos := cfg.Storage.TOS
	fmt.Printf("存储后端: %s\n", client.ProviderType())
	fmt.Printf("TOS 桶: %s  地域: %s  Endpoint: %s\n", tos.BucketName, tos.Region, tos.Endpoint)
	fmt.Printf("本地文件: %s\n", filePath)
	fmt.Printf("对象键名: %s\n", key)

	url, err := client.UploadFile(context.Background(), filePath, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "上传失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("上传成功: %s\n", url)
}
