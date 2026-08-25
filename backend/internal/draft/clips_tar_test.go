package draft

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDraftClipsTarObjectKey(t *testing.T) {
	got := BuildDraftClipsTarObjectKey("abc-123")
	want := "temp/draft/abc-123/abc-123.tar"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if BuildDraftClipsTarObjectKey("  ") != "temp/draft/unknown/unknown.tar" {
		t.Fatalf("empty job id = %q", BuildDraftClipsTarObjectKey("  "))
	}
}

func TestPackClipsTar(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not available")
	}
	dir := t.TempDir()
	clip0 := filepath.Join(dir, "clip_000.mp4")
	clip1 := filepath.Join(dir, "clip_001.mp4")
	if err := os.WriteFile(clip0, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clip1, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	tarPath, err := PackClipsTar(context.Background(), dir, "task-1", []string{clip0, clip1})
	if err != nil {
		t.Fatalf("PackClipsTar: %v", err)
	}
	want := filepath.Join(dir, "task-1.tar")
	if tarPath != want {
		t.Fatalf("tarPath = %q, want %q", tarPath, want)
	}
	st, err := os.Stat(tarPath)
	if err != nil || st.Size() == 0 {
		t.Fatalf("tar missing or empty: %v", err)
	}

	// 空切片列表应跳过
	empty, err := PackClipsTar(context.Background(), dir, "task-2", nil)
	if err != nil || empty != "" {
		t.Fatalf("empty clips = %q err=%v", empty, err)
	}
}

type recordingUploader struct {
	keys []string
	urls map[string]string
	err  error
}

func (r *recordingUploader) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	r.keys = append(r.keys, objectKey)
	if r.err != nil {
		return "", r.err
	}
	if r.urls != nil {
		if u, ok := r.urls[objectKey]; ok {
			return u, nil
		}
	}
	return "https://oss.example/" + objectKey, nil
}

func TestPackAndUploadClipsTar(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not available")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip_000.mp4")
	if err := os.WriteFile(clip, []byte("clip"), 0o644); err != nil {
		t.Fatal(err)
	}
	up := &recordingUploader{}
	url, err := PackAndUploadClipsTar(context.Background(), up, dir, "job-9", []string{clip})
	if err != nil {
		t.Fatalf("PackAndUploadClipsTar: %v", err)
	}
	wantKey := "temp/draft/job-9/job-9.tar"
	if url != "https://oss.example/"+wantKey {
		t.Fatalf("url = %q", url)
	}
	if len(up.keys) != 1 || up.keys[0] != wantKey {
		t.Fatalf("keys = %v", up.keys)
	}
	if !strings.HasSuffix(url, ".tar") {
		t.Fatalf("url should end with .tar: %s", url)
	}
}
