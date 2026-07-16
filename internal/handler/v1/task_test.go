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
	getFn  func(ctx context.Context, id uint) (*model.Task, error)
	listFn func(ctx context.Context, page, pageSize int, opts service.TaskListOptions) ([]model.Task, int64, error)
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
func (m *mockTaskService) Get(ctx context.Context, id uint) (*model.Task, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return nil, nil
}
func (m *mockTaskService) List(ctx context.Context, page, pageSize int, opts service.TaskListOptions) ([]model.Task, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, page, pageSize, opts)
	}
	return nil, 0, nil
}

// TestTaskHandler_Get_ReturnsDraftURL 验证详情接口返回 draft_url / video_url。
func TestTaskHandler_Get_ReturnsDraftURL(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewTaskHandler(&mockTaskService{
		getFn: func(ctx context.Context, id uint) (*model.Task, error) {
			if id != 7 {
				t.Errorf("id = %d, want 7", id)
			}
			return &model.Task{
				ID: 7, Type: model.TaskTypeDraft, Status: model.TaskStatusCompleted,
				DraftURL: "http://example.com/draft", VideoURL: "https://video.example.com/a.mp4",
			}, nil
		},
	})
	r := newAuthedRouter(secret, handler.GetTask, http.MethodGet, "/tasks/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/tasks/7", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			DraftURL string `json:"draft_url"`
			VideoURL string `json:"video_url"`
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
}
