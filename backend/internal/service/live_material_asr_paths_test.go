package service

import (
	"strings"
	"testing"

	"path/filepath"
)

func TestBuildASRSourceLocalFileName(t *testing.T) {
	got := buildASRSourceLocalFileName("abc")
	want := "asr_abc.src"
	if got != want {
		t.Fatalf("buildASRSourceLocalFileName() = %q, want %q", got, want)
	}
}

func TestBuildASRSourceLocalPath(t *testing.T) {
	got := buildASRSourceLocalPath("/tmp/temp", "abc")
	want := filepath.Join("/tmp/temp", "asr_abc.src")
	if got != want {
		t.Fatalf("buildASRSourceLocalPath() = %q, want %q", got, want)
	}
}

func TestBuildASRLocalFileName(t *testing.T) {
	got := buildASRLocalFileName("550e8400-e29b-41d4-a716-446655440000")
	want := "asr_550e8400-e29b-41d4-a716-446655440000.mp3"
	if got != want {
		t.Fatalf("buildASRLocalFileName() = %q, want %q", got, want)
	}
}

func TestBuildASRLocalPath(t *testing.T) {
	got := buildASRLocalPath("/tmp/temp", "abc")
	want := filepath.Join("/tmp/temp", "asr_abc.mp3")
	if got != want {
		t.Fatalf("buildASRLocalPath() = %q, want %q", got, want)
	}
}

func TestBuildASRObjectKey(t *testing.T) {
	got := buildASRObjectKey("temp", "abc")
	want := "temp/asr_abc.mp3"
	if got != want {
		t.Fatalf("buildASRObjectKey() = %q, want %q", got, want)
	}
}

func TestBuildASRObjectKey_TrimsPrefix(t *testing.T) {
	got := buildASRObjectKey("/temp/", "abc")
	if !strings.HasSuffix(got, "asr_abc.mp3") {
		t.Fatalf("buildASRObjectKey() = %q, want suffix asr_abc.mp3", got)
	}
}
