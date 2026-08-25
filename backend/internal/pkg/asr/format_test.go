package asr

import "testing"

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{"wav 后缀", "https://example.com/test21.wav", "wav", false},
		{"mp3 后缀", "https://example.com/audio/test.mp3", "mp3", false},
		{"mp4 后缀", "https://example.com/video.mp4", "mp4", false},
		{"mov 后缀", "https://example.com/video.mov", "mov", false},
		{"带查询参数", "https://example.com/a.wav?token=abc", "wav", false},
		{"大写后缀", "https://example.com/A.WAV", "wav", false},
		{"无后缀默认 mp4", "https://example.com/media", "mp4", false},
		{"不支持格式", "https://example.com/file.flac", "", true},
		{"空 URL", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectFormat(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DetectFormat() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectFormat() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("DetectFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}
