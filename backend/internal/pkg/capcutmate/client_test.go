package capcutmate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildVideoInfosJSON(t *testing.T) {
	got, err := BuildVideoInfosJSON([]VideoInfo{
		{VideoURL: "http://example.com/a.mp4", Start: 0, End: 1000, Volume: 1},
	})
	if err != nil {
		t.Fatalf("BuildVideoInfosJSON() error = %v", err)
	}
	if !strings.Contains(got, `"video_url":"http://example.com/a.mp4"`) {
		t.Errorf("json = %s", got)
	}
}

func TestClient_CreateDraft_SuccessAndRecord(t *testing.T) {
	var gotPath string
	var gotBody CreateDraftRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(CreateDraftResponse{
			Code: 0, Message: "成功", DraftURL: "http://example.com/draft?id=1",
		})
	}))
	defer srv.Close()

	recordDir := t.TempDir()
	client := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	resp, err := client.CreateDraft(context.Background(), 1080, 1920, recordDir)
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if gotPath != pathCreateDraft {
		t.Errorf("path = %s, want %s", gotPath, pathCreateDraft)
	}
	if gotBody.Width != 1080 || gotBody.Height != 1920 {
		t.Errorf("body = %#v", gotBody)
	}
	if resp.DraftURL == "" {
		t.Fatal("draft_url empty")
	}

	entries, err := os.ReadDir(recordDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("record files = %v err=%v", entries, err)
	}
	if !strings.Contains(entries[0].Name(), "create_draft") {
		t.Errorf("filename = %s", entries[0].Name())
	}
	raw, _ := os.ReadFile(filepath.Join(recordDir, entries[0].Name()))
	if !strings.Contains(string(raw), `"response_http_status": 200`) {
		t.Errorf("record = %s", raw)
	}
}

func TestClient_CreateDraft_BusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(CreateDraftResponse{Code: 1, Message: "失败"})
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := client.CreateDraft(context.Background(), 1080, 1920, "")
	if err == nil || !strings.Contains(err.Error(), "业务失败") {
		t.Fatalf("error = %v, want 业务失败", err)
	}
}

func TestClient_AddVideos_Success(t *testing.T) {
	var got AddVideosRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(AddVideosResponse{
			Code: 0, Message: "成功", DraftURL: got.DraftURL, TrackID: "t1",
		})
	}))
	defer srv.Close()

	infos, _ := BuildVideoInfosJSON([]VideoInfo{{VideoURL: "http://x/a.mp4", Start: 0, End: 5000}})
	client := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	resp, err := client.AddVideos(context.Background(), AddVideosRequest{
		DraftURL:   "http://example.com/draft",
		VideoInfos: infos,
	}, "")
	if err != nil {
		t.Fatalf("AddVideos() error = %v", err)
	}
	if got.ScaleX != 1 || got.Alpha != 1 {
		t.Errorf("defaults not applied: %#v", got)
	}
	if resp.TrackID != "t1" {
		t.Errorf("track_id = %s", resp.TrackID)
	}
}

func TestBuildCaptionsJSON(t *testing.T) {
	got, err := BuildCaptionsJSON([]CaptionItem{
		{Start: 0, End: 5000000, Text: "床前明月光", Keyword: "明月", FontSize: 15},
	})
	if err != nil {
		t.Fatalf("BuildCaptionsJSON() error = %v", err)
	}
	if !strings.Contains(got, `"text":"床前明月光"`) {
		t.Errorf("json = %s", got)
	}
	if !strings.Contains(got, `"keyword":"明月"`) {
		t.Errorf("json missing keyword: %s", got)
	}
}

func TestClient_AddCaptions_Success(t *testing.T) {
	var got AddCaptionsRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathAddCaptions {
			t.Errorf("path = %s, want %s", r.URL.Path, pathAddCaptions)
		}
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(AddCaptionsResponse{
			Code: 0, Message: "成功", DraftURL: got.DraftURL, TrackID: "cap-1",
			TextIDs: []string{"t1"}, SegmentIDs: []string{"s1"},
		})
	}))
	defer srv.Close()

	captions, _ := BuildCaptionsJSON([]CaptionItem{{Start: 0, End: 1000, Text: "你好"}})
	client := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	resp, err := client.AddCaptions(context.Background(), AddCaptionsRequest{
		DraftURL: "http://example.com/draft",
		Captions: captions,
	}, "")
	if err != nil {
		t.Fatalf("AddCaptions() error = %v", err)
	}
	if got.Alpha != 1 || got.ScaleX != 1 || got.ScaleY != 1 {
		t.Errorf("defaults not applied: %#v", got)
	}
	if got.Font != "得意黑" || got.FontSize != 13 || got.TextColor != "#ffde00" || got.BorderColor != "#000000" {
		t.Errorf("style defaults = font=%q size=%d text=%q border=%q", got.Font, got.FontSize, got.TextColor, got.BorderColor)
	}
	if resp.TrackID != "cap-1" || len(resp.TextIDs) != 1 {
		t.Errorf("resp = %#v", resp)
	}
}

func TestClient_AddCaptions_BusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(AddCaptionsResponse{Code: 1, Message: "失败"})
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := client.AddCaptions(context.Background(), AddCaptionsRequest{
		DraftURL: "http://d", Captions: "[]",
	}, "")
	if err == nil || !strings.Contains(err.Error(), "业务失败") {
		t.Fatalf("error = %v, want 业务失败", err)
	}
}

func TestClient_CreateDraft_HTTPErrorRecorded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(""))
	}))
	defer srv.Close()

	recordDir := t.TempDir()
	client := NewClient(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := client.CreateDraft(context.Background(), 1080, 1920, recordDir)
	if err == nil {
		t.Fatal("expected error")
	}
	entries, _ := os.ReadDir(recordDir)
	if len(entries) != 1 {
		t.Fatalf("want 1 record, got %d", len(entries))
	}
	raw, _ := os.ReadFile(filepath.Join(recordDir, entries[0].Name()))
	if !strings.Contains(string(raw), `"response_http_status": 502`) {
		t.Errorf("record = %s", raw)
	}
}
