package capcutmate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_GenerateVideoAndWait_Success(t *testing.T) {
	var genCalls, statusCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		switch r.URL.Path {
		case pathGenVideo:
			genCalls.Add(1)
			var req GenVideoRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.APIKey != "test-key" || req.DraftURL == "" {
				t.Errorf("gen_video req = %#v", req)
			}
			_ = json.NewEncoder(w).Encode(GenVideoResponse{Code: 0, Message: "ok"})
		case pathGenVideoStatus:
			n := statusCalls.Add(1)
			status := GenVideoStatusProcessing
			progress := 50
			videoURL := ""
			if n >= 2 {
				status = GenVideoStatusCompleted
				progress = 100
				videoURL = "https://example.com/out.mp4"
			}
			_ = json.NewEncoder(w).Encode(GenVideoStatusResponse{
				Code: 0, Message: "成功", DraftURL: "http://draft",
				Status: status, Progress: progress, VideoURL: videoURL,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(Config{
		BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client(),
		PollInterval: time.Millisecond, MaxPolls: 10,
	})
	videoURL, err := client.GenerateVideoAndWait(context.Background(), "http://draft", "", nil)
	if err != nil {
		t.Fatalf("GenerateVideoAndWait() error = %v", err)
	}
	if videoURL != "https://example.com/out.mp4" {
		t.Errorf("videoURL = %q", videoURL)
	}
	if genCalls.Load() != 1 || statusCalls.Load() < 2 {
		t.Errorf("calls gen=%d status=%d", genCalls.Load(), statusCalls.Load())
	}
}

func TestClient_GenerateVideoAndWait_FailedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		switch r.URL.Path {
		case pathGenVideo:
			_ = json.NewEncoder(w).Encode(GenVideoResponse{Code: 0, Message: "ok"})
		case pathGenVideoStatus:
			_ = json.NewEncoder(w).Encode(GenVideoStatusResponse{
				Code: 0, Status: GenVideoStatusFailed, ErrorMessage: "render error",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(Config{
		BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(),
		PollInterval: time.Millisecond, MaxPolls: 5,
	})
	_, err := client.GenerateVideoAndWait(context.Background(), "http://draft", "", nil)
	if err == nil || !strings.Contains(err.Error(), "render error") {
		t.Fatalf("error = %v, want render error", err)
	}
}

func TestClient_GenVideo_MissingAPIKey(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://example.com"})
	_, err := client.GenVideo(context.Background(), "http://draft", "")
	if err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("error = %v, want API Key", err)
	}
}

func TestClient_GenVideo_OverrideBaseURL(t *testing.T) {
	var gotGenPath, gotStatusPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		switch r.URL.Path {
		case pathGenVideo:
			gotGenPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(GenVideoResponse{Code: 0, Message: "ok"})
		case pathGenVideoStatus:
			gotStatusPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(GenVideoStatusResponse{
				Code: 0, Status: GenVideoStatusCompleted, Progress: 100,
				VideoURL: "https://example.com/out.mp4",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(Config{
		BaseURL:         "http://unused.invalid",
		APIKey:          "k",
		GenVideoBaseURL: srv.URL,
		HTTPClient:      srv.Client(),
		PollInterval:    time.Millisecond,
		MaxPolls:        3,
	})
	videoURL, err := client.GenerateVideoAndWait(context.Background(), "http://draft", "", nil)
	if err != nil {
		t.Fatalf("GenerateVideoAndWait() error = %v", err)
	}
	if videoURL != "https://example.com/out.mp4" {
		t.Errorf("videoURL = %q", videoURL)
	}
	if gotGenPath != pathGenVideo {
		t.Errorf("gen path = %q, want %s", gotGenPath, pathGenVideo)
	}
	if gotStatusPath != pathGenVideoStatus {
		t.Errorf("status path = %q, want %s", gotStatusPath, pathGenVideoStatus)
	}
}
