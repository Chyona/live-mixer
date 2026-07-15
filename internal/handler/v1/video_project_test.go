package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/service"
	jwtpkg "live-mixer/pkg/jwt"
)

// mockVideoProjectService 用于 handler 单元测试。
type mockVideoProjectService struct {
	createFn func(ctx context.Context, createdBy uint, input service.CreateVideoProjectInput) (*model.VideoProject, error)
	updateFn func(ctx context.Context, id uint, input service.VideoProjectUpdateInput) (*model.VideoProject, error)
	deleteFn func(ctx context.Context, id uint) error
	listFn   func(ctx context.Context, page, pageSize int, opts service.VideoProjectListOptions) ([]model.VideoProject, int64, error)
	getFn    func(ctx context.Context, id uint) (*model.VideoProject, error)
}

func (m *mockVideoProjectService) Create(ctx context.Context, createdBy uint, input service.CreateVideoProjectInput) (*model.VideoProject, error) {
	if m.createFn != nil {
		return m.createFn(ctx, createdBy, input)
	}
	return nil, nil
}

func (m *mockVideoProjectService) Update(ctx context.Context, id uint, input service.VideoProjectUpdateInput) (*model.VideoProject, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, input)
	}
	return nil, nil
}

func (m *mockVideoProjectService) Delete(ctx context.Context, id uint) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockVideoProjectService) List(ctx context.Context, page, pageSize int, opts service.VideoProjectListOptions) ([]model.VideoProject, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, page, pageSize, opts)
	}
	return nil, 0, nil
}

func (m *mockVideoProjectService) Get(ctx context.Context, id uint) (*model.VideoProject, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return nil, nil
}

// TestVideoProjectHandler_Create_Success 验证创建接口成功返回完整项目（含结构化 clips）。
func TestVideoProjectHandler_Create_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		createFn: func(ctx context.Context, createdBy uint, input service.CreateVideoProjectInput) (*model.VideoProject, error) {
			if createdBy != 3 || input.LiveID != 5 {
				t.Errorf("createdBy/liveID = %d/%d, want 3/5", createdBy, input.LiveID)
			}
			if input.PromptID != 0 {
				t.Errorf("promptID = %d, want 0 (handler passes request value)", input.PromptID)
			}
			if len(input.Clips0) != 1 || input.Clips0[0].EndTime != 1000 {
				t.Errorf("Clips0 = %#v", input.Clips0)
			}
			if len(input.Clips1) != 1 || input.Clips1[0].Text != "我是中国人" {
				t.Errorf("Clips1 = %#v", input.Clips1)
			}
			return &model.VideoProject{
				ID:        1,
				Name:      input.Name,
				Remark:    input.Remark,
				LiveID:    input.LiveID,
				PromptID:  1,
				Clips0:    `[{"start_time":0,"end_time":1000}]`,
				Clips1:    `[{"text":"我是中国人","start_time":0,"end_time":1000}]`,
				CreatedBy: createdBy,
			}, nil
		},
	})
	r := newAuthedRouter(secret, handler.CreateVideoProject, http.MethodPost, "/video-projects")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 3, Username: "admin"})

	body := []byte(`{
		"name":"剪辑项目",
		"live_id":5,
		"remark":"备注",
		"clips0":[{"start_time":0,"end_time":1000}],
		"clips1":[{"text":"我是中国人","start_time":0,"end_time":1000}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/video-projects", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			ID       uint            `json:"id"`
			Name     string          `json:"name"`
			Remark   string          `json:"remark"`
			LiveID   uint            `json:"live_id"`
			PromptID uint            `json:"prompt_id"`
			Clips0   json.RawMessage `json:"clips0"`
			Clips1   json.RawMessage `json:"clips1"`
			DraftURL string          `json:"draft_url"`
			VideoURL string          `json:"video_url"`
			CreatedBy uint           `json:"created_by"`
			Ext      string          `json:"ext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 || resp.Data.ID != 1 || resp.Data.Name != "剪辑项目" {
		t.Fatalf("unexpected data: %+v", resp.Data)
	}
	if resp.Data.PromptID != 1 || resp.Data.CreatedBy != 3 || resp.Data.LiveID != 5 {
		t.Fatalf("unexpected ids: %+v", resp.Data)
	}
	var clips0 []model.ClipRange
	if err := json.Unmarshal(resp.Data.Clips0, &clips0); err != nil || len(clips0) != 1 || clips0[0].EndTime != 1000 {
		t.Fatalf("clips0 = %s, err=%v", string(resp.Data.Clips0), err)
	}
	var clips1 []model.ClipWithText
	if err := json.Unmarshal(resp.Data.Clips1, &clips1); err != nil || len(clips1) != 1 || clips1[0].Text != "我是中国人" {
		t.Fatalf("clips1 = %s, err=%v", string(resp.Data.Clips1), err)
	}
}

// TestVideoProjectHandler_List_WithFilters 验证列表筛选参数传递到 service。
func TestVideoProjectHandler_List_WithFilters(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		listFn: func(ctx context.Context, page, pageSize int, opts service.VideoProjectListOptions) ([]model.VideoProject, int64, error) {
			if opts.Keywords != "发布会,2026" || opts.StartDate != "2026-01-01" {
				t.Errorf("unexpected opts: %+v", opts)
			}
			return nil, 0, nil
		},
	})
	r := newAuthedRouter(secret, handler.ListVideoProjects, http.MethodGet, "/video-projects")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/video-projects?keywords=发布会,2026&start_date=2026-01-01", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestVideoProjectHandler_Update_Partial 验证部分字段更新（未传 clips 不触发更新）。
func TestVideoProjectHandler_Update_Partial(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		updateFn: func(ctx context.Context, id uint, input service.VideoProjectUpdateInput) (*model.VideoProject, error) {
			if id != 2 || input.VideoURL == nil || *input.VideoURL != "https://video.example.com/a.mp4" {
				t.Errorf("unexpected update input: id=%d input=%+v", id, input)
			}
			if input.Clips0 != nil || input.Clips1 != nil {
				t.Errorf("clips should be nil when omitted, got clips0=%v clips1=%v", input.Clips0, input.Clips1)
			}
			return &model.VideoProject{ID: 2, VideoURL: *input.VideoURL}, nil
		},
	})
	r := newAuthedRouter(secret, handler.UpdateVideoProject, http.MethodPut, "/video-projects/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	body := []byte(`{"video_url":"https://video.example.com/a.mp4"}`)
	req := httptest.NewRequest(http.MethodPut, "/video-projects/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestVideoProjectHandler_Update_WithClips 验证显式传入 clips 数组时会传给 service。
func TestVideoProjectHandler_Update_WithClips(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		updateFn: func(ctx context.Context, id uint, input service.VideoProjectUpdateInput) (*model.VideoProject, error) {
			if input.Clips0 == nil || len(*input.Clips0) != 1 {
				t.Fatalf("Clips0 = %#v", input.Clips0)
			}
			if input.Clips1 == nil || (*input.Clips1)[0].Text != "我是中国人" {
				t.Fatalf("Clips1 = %#v", input.Clips1)
			}
			return &model.VideoProject{ID: id}, nil
		},
	})
	r := newAuthedRouter(secret, handler.UpdateVideoProject, http.MethodPut, "/video-projects/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	body := []byte(`{
		"clips0":[{"start_time":0,"end_time":1000}],
		"clips1":[{"text":"我是中国人","start_time":0,"end_time":1000}]
	}`)
	req := httptest.NewRequest(http.MethodPut, "/video-projects/3", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestVideoProjectHandler_Delete_Success 验证删除成功。
func TestVideoProjectHandler_Delete_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		deleteFn: func(ctx context.Context, id uint) error {
			if id != 4 {
				t.Errorf("id = %d, want 4", id)
			}
			return nil
		},
	})
	r := newAuthedRouter(secret, handler.DeleteVideoProject, http.MethodDelete, "/video-projects/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodDelete, "/video-projects/4", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Message != "删除成功" {
		t.Errorf("message = %q, want 删除成功", resp.Message)
	}
}

// TestVideoProjectHandler_Get_NotFound 验证详情不存在时返回 404。
func TestVideoProjectHandler_Get_NotFound(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		getFn: func(ctx context.Context, id uint) (*model.VideoProject, error) {
			return nil, service.ErrVideoProjectNotFound
		},
	})
	r := newAuthedRouter(secret, handler.GetVideoProject, http.MethodGet, "/video-projects/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/video-projects/99", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
