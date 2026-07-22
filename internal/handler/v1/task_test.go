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
	getFn  func(ctx context.Context, id string) (*model.Task, error)
	listFn func(ctx context.Context, page, pageSize int, opts service.TaskListOptions) ([]model.TaskListItem, int64, error)
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

// TestTaskHandler_Get_ReturnsDraftURL 验证详情接口返回 draft_url / video_url / live_url / width / height。
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
				LiveURL: "https://example.com/live.mp4", Width: 1080, Height: 1920,
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
			DraftURL  string `json:"draft_url"`
			VideoURL  string `json:"video_url"`
			LiveURL   string `json:"live_url"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			CreatedBy string `json:"created_by"`
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
	if resp.Data.LiveURL != "https://example.com/live.mp4" {
		t.Errorf("live_url = %q", resp.Data.LiveURL)
	}
	if resp.Data.Width != 1080 || resp.Data.Height != 1920 {
		t.Errorf("width/height = %d/%d", resp.Data.Width, resp.Data.Height)
	}
	if resp.Data.CreatedBy != "AdminNick" {
		t.Errorf("created_by = %q, want AdminNick", resp.Data.CreatedBy)
	}
}

// TestTaskHandler_List_ReturnsLiveURLAndCanvasSize 验证列表接口返回 live_url / width / height。
func TestTaskHandler_List_ReturnsLiveURLAndCanvasSize(t *testing.T) {
	secret := "handler-test-secret"
	projectID := uint(9)
	handler := NewTaskHandler(&mockTaskService{
		listFn: func(ctx context.Context, page, pageSize int, opts service.TaskListOptions) ([]model.TaskListItem, int64, error) {
			return []model.TaskListItem{{
				ID: "22222222-2222-2222-2222-222222222222", Type: model.TaskTypeDraft,
				Status: model.TaskStatusCompleted, CreatedBy: 1,
				VideoProjectID: &projectID, VideoProjectName: "精剪",
				LiveURL: "https://example.com/live.mp4", Width: 1080, Height: 1920,
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
				LiveURL string `json:"live_url"`
				Width   int    `json:"width"`
				Height  int    `json:"height"`
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
	if item.Width != 1080 || item.Height != 1920 {
		t.Errorf("width/height = %d/%d, want 1080/1920", item.Width, item.Height)
	}
}
