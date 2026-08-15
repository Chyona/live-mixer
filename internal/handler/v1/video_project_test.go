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
	createFn             func(ctx context.Context, createdBy uint, input service.CreateVideoProjectInput) (*model.VideoProject, error)
	updateFn             func(ctx context.Context, id uint, input service.VideoProjectUpdateInput) (*model.VideoProject, error)
	deleteFn             func(ctx context.Context, id uint) error
	listFn               func(ctx context.Context, page, pageSize int, opts service.VideoProjectListOptions) ([]model.VideoProjectListItem, int64, error)
	listByLiveMaterialFn func(ctx context.Context, liveID uint, page, pageSize int) ([]model.VideoProjectListItem, int64, error)
	getFn                func(ctx context.Context, id uint) (*model.VideoProject, error)
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

func (m *mockVideoProjectService) List(ctx context.Context, page, pageSize int, opts service.VideoProjectListOptions) ([]model.VideoProjectListItem, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, page, pageSize, opts)
	}
	return nil, 0, nil
}

func (m *mockVideoProjectService) ListByLiveMaterial(ctx context.Context, liveID uint, page, pageSize int) ([]model.VideoProjectListItem, int64, error) {
	if m.listByLiveMaterialFn != nil {
		return m.listByLiveMaterialFn(ctx, liveID, page, pageSize)
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
				Clips0:    []model.ClipRange{{StartTime: 0, EndTime: 1000}},
				Clips1:    []model.ClipWithText{{Text: "我是中国人", StartTime: 0, EndTime: 1000}},
				CreatedBy: createdBy,
			}, nil
		},
	}, accountsStub(&model.Account{ID: 3, Username: "admin", Nickname: "AdminNick"}))
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
			ID        uint                 `json:"id"`
			Name      string               `json:"name"`
			Remark    string               `json:"remark"`
			LiveID    uint                 `json:"live_id"`
			PromptID  uint                 `json:"prompt_id"`
			Clips0    []model.ClipRange    `json:"clips0"`
			Clips1    []model.ClipWithText `json:"clips1"`
			CreatedBy string               `json:"created_by"`
			Ext       string               `json:"ext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 || resp.Data.ID != 1 || resp.Data.Name != "剪辑项目" {
		t.Fatalf("unexpected data: %+v", resp.Data)
	}
	if resp.Data.PromptID != 1 || resp.Data.CreatedBy != "AdminNick" || resp.Data.LiveID != 5 {
		t.Fatalf("unexpected ids: %+v", resp.Data)
	}
	if len(resp.Data.Clips0) != 1 || resp.Data.Clips0[0].EndTime != 1000 {
		t.Fatalf("clips0 = %#v", resp.Data.Clips0)
	}
	if len(resp.Data.Clips1) != 1 || resp.Data.Clips1[0].Text != "我是中国人" {
		t.Fatalf("clips1 = %#v", resp.Data.Clips1)
	}
}

// TestVideoProjectHandler_List_WithFilters 验证列表筛选参数传递到 service。
func TestVideoProjectHandler_List_WithFilters(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		listFn: func(ctx context.Context, page, pageSize int, opts service.VideoProjectListOptions) ([]model.VideoProjectListItem, int64, error) {
			if opts.Keywords != "发布会,2026" || opts.StartDate != "2026-01-01" {
				t.Errorf("unexpected opts: %+v", opts)
			}
			return []model.VideoProjectListItem{{
				ID: 1, Name: "项目", LiveID: 5, LiveName: "春季发布会", PromptID: 1,
				Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
				TaskCount: 3,
			}}, 1, nil
		},
	}, nil)
	r := newAuthedRouter(secret, handler.ListVideoProjects, http.MethodGet, "/video-projects")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/video-projects?keywords=发布会,2026&start_date=2026-01-01", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp struct {
		Data struct {
			List []struct {
				LiveName  string `json:"live_name"`
				LiveID    uint   `json:"live_id"`
				TaskCount int64  `json:"task_count"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data.List) != 1 {
		t.Fatalf("list = %+v", resp.Data.List)
	}
	if resp.Data.List[0].LiveID != 5 || resp.Data.List[0].LiveName != "春季发布会" {
		t.Fatalf("live fields = %+v", resp.Data.List[0])
	}
	if resp.Data.List[0].TaskCount != 3 {
		t.Fatalf("task_count = %d, want 3", resp.Data.List[0].TaskCount)
	}
}

// TestVideoProjectHandler_ListByLiveMaterial_Success 验证按素材 ID 查询关联项目并分页。
func TestVideoProjectHandler_ListByLiveMaterial_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		listByLiveMaterialFn: func(ctx context.Context, liveID uint, page, pageSize int) ([]model.VideoProjectListItem, int64, error) {
			if liveID != 7 || page != 2 || pageSize != 5 {
				t.Errorf("liveID/page/pageSize = %d/%d/%d, want 7/2/5", liveID, page, pageSize)
			}
			return []model.VideoProjectListItem{{
				ID: 11, Name: "素材关联项目", LiveID: 7, LiveName: "春季发布会",
				Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
				TaskCount: 2,
			}}, 12, nil
		},
	}, nil)
	r := newAuthedRouter(secret, handler.ListVideoProjectsByLiveMaterial, http.MethodGet, "/live-materials/:id/video-projects")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials/7/video-projects?page=2&page_size=5", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp struct {
		Data struct {
			List []struct {
				ID       uint   `json:"id"`
				Name     string `json:"name"`
				LiveID   uint   `json:"live_id"`
				LiveName string `json:"live_name"`
			} `json:"list"`
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data.Total != 12 || resp.Data.Page != 2 || resp.Data.PageSize != 5 {
		t.Fatalf("page data = %+v", resp.Data)
	}
	if len(resp.Data.List) != 1 || resp.Data.List[0].ID != 11 || resp.Data.List[0].LiveID != 7 {
		t.Fatalf("list = %+v", resp.Data.List)
	}
}

// TestVideoProjectHandler_ListByLiveMaterial_NotFound 验证素材不存在时返回 404。
func TestVideoProjectHandler_ListByLiveMaterial_NotFound(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		listByLiveMaterialFn: func(ctx context.Context, liveID uint, page, pageSize int) ([]model.VideoProjectListItem, int64, error) {
			return nil, 0, service.ErrLiveMaterialNotFound
		},
	}, nil)
	r := newAuthedRouter(secret, handler.ListVideoProjectsByLiveMaterial, http.MethodGet, "/live-materials/:id/video-projects")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials/99/video-projects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", w.Code, w.Body.String())
	}
}

// TestVideoProjectHandler_ListByLiveMaterial_InvalidID 验证非法素材 ID 返回 400。
func TestVideoProjectHandler_ListByLiveMaterial_InvalidID(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{}, nil)
	r := newAuthedRouter(secret, handler.ListVideoProjectsByLiveMaterial, http.MethodGet, "/live-materials/:id/video-projects")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials/abc/video-projects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestVideoProjectHandler_Update_Partial 验证部分字段更新（未传 clips 不触发更新）。
func TestVideoProjectHandler_Update_Partial(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewVideoProjectHandler(&mockVideoProjectService{
		updateFn: func(ctx context.Context, id uint, input service.VideoProjectUpdateInput) (*model.VideoProject, error) {
			if id != 2 || input.ProjectSource == nil || *input.ProjectSource != "manual" {
				t.Errorf("unexpected update input: id=%d input=%+v", id, input)
			}
			if input.Clips0 != nil || input.Clips1 != nil {
				t.Errorf("clips should be nil when omitted, got clips0=%v clips1=%v", input.Clips0, input.Clips1)
			}
			return &model.VideoProject{ID: 2, ProjectSource: *input.ProjectSource}, nil
		},
	}, nil)
	r := newAuthedRouter(secret, handler.UpdateVideoProject, http.MethodPut, "/video-projects/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	body := []byte(`{"project_source":"manual"}`)
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
	}, nil)
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
	}, nil)
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
	}, nil)
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
