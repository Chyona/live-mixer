package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/service"
	jwtpkg "live-mixer/pkg/jwt"
)

// mockTaskService 用于任务 handler 单元测试。
type mockTaskService struct {
	getFn         func(ctx context.Context, id string) (*model.Task, error)
	listFn        func(ctx context.Context, page, pageSize int, opts service.TaskListOptions) ([]model.TaskListItem, int64, error)
	listRunningFn func(ctx context.Context, videoProjectID uint, taskType string, activeOnly bool) ([]model.TaskListItem, int64, error)
}

func (m *mockTaskService) CreateAISlice(ctx context.Context, createdBy uint, input service.CreateAISliceInput) (*model.Task, error) {
	return nil, nil
}
func (m *mockTaskService) CreateDraft(ctx context.Context, createdBy uint, input service.CreateDraftInput) (*model.Task, error) {
	return nil, nil
}
func (m *mockTaskService) CreateAISliceDraft(ctx context.Context, createdBy uint, input service.CreateAISliceDraftInput) (*model.Task, error) {
	return nil, nil
}
func (m *mockTaskService) Get(ctx context.Context, id string) (*model.Task, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return nil, nil
}
func (m *mockTaskService) List(ctx context.Context, page, pageSize int, opts service.TaskListOptions) ([]model.TaskListItem, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, page, pageSize, opts)
	}
	return nil, 0, nil
}
func (m *mockTaskService) ListRunningByVideoProject(ctx context.Context, videoProjectID uint, taskType string, activeOnly bool) ([]model.TaskListItem, int64, error) {
	if m.listRunningFn != nil {
		return m.listRunningFn(ctx, videoProjectID, taskType, activeOnly)
	}
	return nil, 0, nil
}

// TestTaskHandler_Get_ReturnsDraftURL 验证详情接口返回 draft_url / video_url / clips_tar_url / live_url / live_name / width / height。
func TestTaskHandler_Get_ReturnsDraftURL(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewTaskHandler(&mockTaskService{
		getFn: func(ctx context.Context, id string) (*model.Task, error) {
			wantID := "11111111-1111-1111-1111-111111111111"
			if id != wantID {
				t.Errorf("id = %q, want %q", id, wantID)
			}
			return &model.Task{
				ID: wantID, Type: model.TaskTypeDraft, Status: model.TaskStatusCompleted, CreatedBy: 1,
				DraftURL: "http://example.com/draft", VideoURL: "https://video.example.com/a.mp4",
				ClipsTarURL: "https://oss.example/temp/draft/" + wantID + "/" + wantID + ".tar",
				LiveURL: "https://example.com/live.mp4", LiveName: "春季发布会", Width: 1080, Height: 1920,
			}, nil
		},
	}, accountsStub(&model.Account{ID: 1, Username: "admin", Nickname: "AdminNick"}))
	r := newAuthedRouter(secret, handler.GetTask, http.MethodGet, "/tasks/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/tasks/11111111-1111-1111-1111-111111111111", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			DraftURL    string `json:"draft_url"`
			VideoURL    string `json:"video_url"`
			ClipsTarURL string `json:"clips_tar_url"`
			LiveURL     string `json:"live_url"`
			LiveName    string `json:"live_name"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
			CreatedBy   string `json:"created_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data.DraftURL != "http://example.com/draft" {
		t.Errorf("draft_url = %q", resp.Data.DraftURL)
	}
	if resp.Data.VideoURL != "https://video.example.com/a.mp4" {
		t.Errorf("video_url = %q", resp.Data.VideoURL)
	}
	wantTar := "https://oss.example/temp/draft/11111111-1111-1111-1111-111111111111/11111111-1111-1111-1111-111111111111.tar"
	if resp.Data.ClipsTarURL != wantTar {
		t.Errorf("clips_tar_url = %q", resp.Data.ClipsTarURL)
	}
	if resp.Data.LiveURL != "https://example.com/live.mp4" {
		t.Errorf("live_url = %q", resp.Data.LiveURL)
	}
	if resp.Data.LiveName != "春季发布会" {
		t.Errorf("live_name = %q", resp.Data.LiveName)
	}
	if resp.Data.Width != 1080 || resp.Data.Height != 1920 {
		t.Errorf("width/height = %d/%d", resp.Data.Width, resp.Data.Height)
	}
	if resp.Data.CreatedBy != "AdminNick" {
		t.Errorf("created_by = %q, want AdminNick", resp.Data.CreatedBy)
	}
}

// TestTaskHandler_List_ReturnsLiveURLAndCanvasSize 验证列表接口返回 live_url / live_name / width / height。
func TestTaskHandler_List_ReturnsLiveURLAndCanvasSize(t *testing.T) {
	secret := "handler-test-secret"
	projectID := uint(9)
	handler := NewTaskHandler(&mockTaskService{
		listFn: func(ctx context.Context, page, pageSize int, opts service.TaskListOptions) ([]model.TaskListItem, int64, error) {
			return []model.TaskListItem{{
				ID: "22222222-2222-2222-2222-222222222222", Type: model.TaskTypeDraft,
				Status: model.TaskStatusCompleted, CreatedBy: 1,
				VideoProjectID: &projectID, VideoProjectName: "精剪",
				LiveURL: "https://example.com/live.mp4", LiveName: "春季发布会", Width: 1080, Height: 1920,
			}}, 1, nil
		},
	}, accountsStub(&model.Account{ID: 1, Username: "admin", Nickname: "AdminNick"}))
	r := newAuthedRouter(secret, handler.ListTasks, http.MethodGet, "/tasks")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/tasks?page=1&page_size=10", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			List []struct {
				LiveURL  string `json:"live_url"`
				LiveName string `json:"live_name"`
				Width    int    `json:"width"`
				Height   int    `json:"height"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data.List) != 1 {
		t.Fatalf("list len = %d, want 1", len(resp.Data.List))
	}
	item := resp.Data.List[0]
	if item.LiveURL != "https://example.com/live.mp4" {
		t.Errorf("live_url = %q", item.LiveURL)
	}
	if item.LiveName != "春季发布会" {
		t.Errorf("live_name = %q", item.LiveName)
	}
	if item.Width != 1080 || item.Height != 1920 {
		t.Errorf("width/height = %d/%d, want 1080/1920", item.Width, item.Height)
	}
}

// TestTaskHandler_ListRunningTasksByProject 验证项目运行中任务接口传参与响应。
func TestTaskHandler_ListRunningTasksByProject(t *testing.T) {
	secret := "handler-test-secret"
	projectID := uint(12)
	handler := NewTaskHandler(&mockTaskService{
		listRunningFn: func(ctx context.Context, videoProjectID uint, taskType string, activeOnly bool) ([]model.TaskListItem, int64, error) {
			if videoProjectID != 12 {
				t.Errorf("videoProjectID = %d, want 12", videoProjectID)
			}
			if taskType != model.TaskTypeDraft {
				t.Errorf("taskType = %q, want draft", taskType)
			}
			if !activeOnly {
				t.Error("activeOnly = false, want true")
			}
			return []model.TaskListItem{{
				ID: "33333333-3333-3333-3333-333333333333", Type: model.TaskTypeDraft,
				Status: model.TaskStatusProcessing, Progress: 40, CreatedBy: 1,
				VideoProjectID: &projectID, VideoProjectName: "精剪",
				LiveURL: "https://example.com/live.mp4", LiveName: "春季发布会", Width: 1080, Height: 1920,
			}}, 1, nil
		},
	}, accountsStub(&model.Account{ID: 1, Username: "admin", Nickname: "AdminNick"}))
	r := newAuthedRouter(secret, handler.ListRunningTasksByProject, http.MethodGet, "/video-projects/:id/running-tasks")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/video-projects/12/running-tasks?type=draft&active_only=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			List []struct {
				ID       string `json:"id"`
				Status   string `json:"status"`
				Progress int16  `json:"progress"`
				LiveURL  string `json:"live_url"`
				CreatedBy string `json:"created_by"`
			} `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data.Total != 1 || len(resp.Data.List) != 1 {
		t.Fatalf("total/list = %d/%d, want 1/1", resp.Data.Total, len(resp.Data.List))
	}
	item := resp.Data.List[0]
	if item.Status != model.TaskStatusProcessing || item.Progress != 40 {
		t.Errorf("status/progress = %s/%d", item.Status, item.Progress)
	}
	if item.LiveURL != "https://example.com/live.mp4" {
		t.Errorf("live_url = %q", item.LiveURL)
	}
	if item.CreatedBy != "AdminNick" {
		t.Errorf("created_by = %q, want AdminNick", item.CreatedBy)
	}
}

// TestTaskHandler_ListRunningTasksByProject_NotFound 验证项目不存在时返回 404。
func TestTaskHandler_ListRunningTasksByProject_NotFound(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewTaskHandler(&mockTaskService{
		listRunningFn: func(ctx context.Context, videoProjectID uint, taskType string, activeOnly bool) ([]model.TaskListItem, int64, error) {
			return nil, 0, service.ErrVideoProjectNotFound
		},
	}, accountsStub())
	r := newAuthedRouter(secret, handler.ListRunningTasksByProject, http.MethodGet, "/video-projects/:id/running-tasks")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/video-projects/99/running-tasks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", w.Code, w.Body.String())
	}
}
