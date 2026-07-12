package util

import (
	"fmt"
	"os"
	"testing"
)

func TestDownloadFilePlus(t *testing.T) {
	url := "https://gogoshine.com/min.mp4"
	// url := "https://lf9-appstore-sign.oceancloudapi.com/ocean-cloud-tos/VolcanoUserVoice/speech_7468512265134932019_8e808c47-ea97-4327-80c5-e4b05a7279e0.mp3?lk3s=da27ec82&x-expires=1750476972&x-signature=esXJqQPhgumarCxP%2FHBZxZf%2F4Ik%3D"
	saveDir := "D:/"
	file, ext, err := DownloadFilePlus(url, saveDir)
	if err != nil {
		t.Errorf("DownloadFilePlus failed: %v", err)
	}

	fmt.Printf("download file: %s, ext: %s\n", file, ext)

	defer os.Remove(file)

	// 检查文件是否存在
	if _, err = os.Stat(file); os.IsNotExist(err) {
		t.Errorf("downloaded file does not exist: %v", err)
	}

	// 检查文件大小
	info, err := os.Stat(file)
	if err != nil {
		t.Errorf("get file info failed: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("downloaded file size is 0")
	}

	fmt.Printf("download file size: %d\n", info.Size())
}
