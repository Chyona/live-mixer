package storage

import (
	"context"
	"testing"
)

func TestResolveBasePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", DefaultBasePath},
		{"video_editing", "video_editing"},
		{"/video_editing/", "video_editing"},
		{"custom/path", "custom/path"},
	}
	for _, tt := range tests {
		if got := ResolveBasePath(tt.input); got != tt.want {
			t.Errorf("ResolveBasePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestJoinObjectKey(t *testing.T) {
	tests := []struct {
		basePath  string
		objectKey string
		want      string
	}{
		{"video_editing", "test/main.go", "video_editing/test/main.go"},
		{"video_editing", "/temp/1.wav", "video_editing/temp/1.wav"},
		{"", "plain.txt", "plain.txt"},
		{"video_editing", "", "video_editing"},
	}
	for _, tt := range tests {
		if got := JoinObjectKey(tt.basePath, tt.objectKey); got != tt.want {
			t.Errorf("JoinObjectKey(%q, %q) = %q, want %q", tt.basePath, tt.objectKey, got, tt.want)
		}
	}
}

func TestClient_UploadFile_WithBasePath(t *testing.T) {
	var gotKey string
	client := &Client{
		basePath: "video_editing",
		provider: &mockStorageProvider{
			uploadFileFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				gotKey = objectKey
				return "https://cdn.example.com/" + objectKey, nil
			},
		},
	}

	_, err := client.UploadFile(context.Background(), "/tmp/a.txt", "test/a.txt")
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if gotKey != "video_editing/test/a.txt" {
		t.Errorf("objectKey = %q, want video_editing/test/a.txt", gotKey)
	}
}

func TestClient_ObjectKey(t *testing.T) {
	client := &Client{basePath: "video_editing"}
	if got := client.ObjectKey("temp", "1", "a.wav"); got != "video_editing/temp/1/a.wav" {
		t.Errorf("ObjectKey() = %q, want video_editing/temp/1/a.wav", got)
	}
}

func TestClient_TempObjectKey(t *testing.T) {
	client := &Client{basePath: "video_editing"}
	if got := client.TempObjectKey("12", "a.wav"); got != "video_editing/temp/12/a.wav" {
		t.Errorf("TempObjectKey() = %q, want video_editing/temp/12/a.wav", got)
	}
}

func TestClient_TestObjectKey(t *testing.T) {
	client := &Client{basePath: "video_editing"}
	if got := client.TestObjectKey("main.go"); got != "video_editing/test/main.go" {
		t.Errorf("TestObjectKey() = %q, want video_editing/test/main.go", got)
	}
}
