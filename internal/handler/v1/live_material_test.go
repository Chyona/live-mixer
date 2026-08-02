package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"live-mixer/internal/middleware"
	"live-mixer/internal/model"
	"live-mixer/internal/service"
	jwtpkg "live-mixer/pkg/jwt"

	"github.com/gin-gonic/gin"
)

// mockLiveMaterialService 用于 handler 单元测试。
type mockLiveMaterialService struct {
	createFn               func(ctx context.Context, createdBy uint, name, liveURL, remark, ext string) (*model.LiveMaterial, error)
	updateFn               func(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error)
	deleteFn               func(ctx context.Context, id uint) error
	listFn                 func(ctx context.Context, page, pageSize int, opts service.LiveMaterialListOptions) ([]model.LiveMaterialListItem, int64, error)
	getFn                  func(ctx context.Context, id uint) (*model.LiveMaterial, error)
	downloadASRSubtitleFn  func(ctx context.Context, id uint) ([]byte, string, error)
	retryASRFn             func(ctx context.Context, id uint) (*model.LiveMaterial, error)
}

func (m *mockLiveMaterialService) Create(ctx context.Context, createdBy uint, name, liveURL, remark, ext string) (*model.LiveMaterial, error) {
	if m.createFn != nil {
		return m.createFn(ctx, createdBy, name, liveURL, remark, ext)
	}
	return nil, nil
}

func (m *mockLiveMaterialService) Update(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, name, remark)
	}
	return nil, nil
}

func (m *mockLiveMaterialService) List(ctx context.Context, page, pageSize int, opts service.LiveMaterialListOptions) ([]model.LiveMaterialListItem, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, page, pageSize, opts)
	}
	return nil, 0, nil
}

func (m *mockLiveMaterialService) Get(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return nil, nil
}

func (m *mockLiveMaterialService) Delete(ctx context.Context, id uint) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockLiveMaterialService) RetryASR(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	if m.retryASRFn != nil {
		return m.retryASRFn(ctx, id)
	}
	return nil, nil
}

func (m *mockLiveMaterialService) DownloadASRSubtitle(ctx context.Context, id uint) ([]byte, string, error) {
	if m.downloadASRSubtitleFn != nil {
		return m.downloadASRSubtitleFn(ctx, id)
	}
	return nil, "", nil
}

// newAuthedRouter 创建带 JWT 鉴权的测试路由。
func newAuthedRouter(secret string, handler gin.HandlerFunc, method, path string) *gin.Engine {
	r := gin.New()
	r.Handle(method, path, middleware.JWTAuth(secret), handler)
	return r
}

// TestLiveMaterialHandler_Get_Success 验证详情接口返回分句格式的 live_asr。
func TestLiveMaterialHandler_Get_Success(t *testing.T) {
	secret := "handler-test-secret"
	rawASR := `{"result":{"utterances":[{"additions":{"speaker":"1"},"end_time":400,"start_time":40,"text":"跳舞吗？","words":[{"end_time":160,"start_time":40,"text":"跳"}]}]}}`
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		getFn: func(ctx context.Context, id uint) (*model.LiveMaterial, error) {
			if id != 7 {
				t.Errorf("id = %d, want 7", id)
			}
			return &model.LiveMaterial{
				ID:        7,
				Name:      "素材详情",
				LiveURL:   "https://example.com/detail.mp4",
				LiveASR:   rawASR,
				ASRStatus: model.ASRStatusCompleted,
			}, nil
		},
	}, nil)

	r := newAuthedRouter(secret, handler.GetLiveMaterial, http.MethodGet, "/live-materials/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials/7", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Code int                          `json:"code"`
		Data LiveMaterialDetailResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data.LiveASR) != 1 {
		t.Fatalf("live_asr len = %d, want 1", len(resp.Data.LiveASR))
	}
	if resp.Data.LiveASR[0].Speaker != "1" || resp.Data.LiveASR[0].Text != "跳舞吗？" {
		t.Errorf("live_asr[0] = %+v", resp.Data.LiveASR[0])
	}
	if len(resp.Data.LiveASR[0].Words) != 1 || resp.Data.LiveASR[0].Words[0].Text != "跳" {
		t.Errorf("live_asr[0].words = %+v", resp.Data.LiveASR[0].Words)
	}
}

// TestLiveMaterialHandler_Get_NotFound 验证素材不存在时返回 404。
func TestLiveMaterialHandler_Get_NotFound(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		getFn: func(ctx context.Context, id uint) (*model.LiveMaterial, error) {
			return nil, service.ErrLiveMaterialNotFound
		},
	}, nil)
	r := newAuthedRouter(secret, handler.GetLiveMaterial, http.MethodGet, "/live-materials/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials/999", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestLiveMaterialHandler_Get_InvalidID 验证非法 ID 返回 400。
func TestLiveMaterialHandler_Get_InvalidID(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{}, nil)
	r := newAuthedRouter(secret, handler.GetLiveMaterial, http.MethodGet, "/live-materials/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials/abc", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestLiveMaterialHandler_List_Success 验证列表接口默认分页且不返回 live_asr。
func TestLiveMaterialHandler_List_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		listFn: func(ctx context.Context, page, pageSize int, opts service.LiveMaterialListOptions) ([]model.LiveMaterialListItem, int64, error) {
			if page != 1 || pageSize != 10 {
				t.Errorf("page/pageSize = %d/%d, want 1/10", page, pageSize)
			}
			return []model.LiveMaterialListItem{
				{
					ID:           1,
					Name:         "素材A",
					LiveURL:      "https://example.com/a.mp4",
					ASRStatus:    model.ASRStatusCompleted,
					ProjectCount: 4,
				},
			}, 1, nil
		},
	}, nil)

	r := newAuthedRouter(secret, handler.ListLiveMaterials, http.MethodGet, "/live-materials")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Name         string `json:"name"`
				CreatedBy    string `json:"created_by"`
				ProjectCount int64  `json:"project_count"`
			} `json:"list"`
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Data.PageSize != 10 {
		t.Errorf("page_size = %d, want 10", resp.Data.PageSize)
	}
	if len(resp.Data.List) != 1 {
		t.Fatalf("list len = %d, want 1", len(resp.Data.List))
	}
	if resp.Data.List[0].Name != "素材A" {
		t.Errorf("name = %q, want 素材A", resp.Data.List[0].Name)
	}
	if resp.Data.List[0].ProjectCount != 4 {
		t.Errorf("project_count = %d, want 4", resp.Data.List[0].ProjectCount)
	}
	if strings.Contains(w.Body.String(), `"live_asr"`) {
		t.Error("response should not contain live_asr field")
	}
}

// TestLiveMaterialHandler_List_CustomPageSize 验证自定义 page_size 生效。
func TestLiveMaterialHandler_List_CustomPageSize(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		listFn: func(ctx context.Context, page, pageSize int, opts service.LiveMaterialListOptions) ([]model.LiveMaterialListItem, int64, error) {
			if pageSize != 5 {
				t.Errorf("pageSize = %d, want 5", pageSize)
			}
			return nil, 0, nil
		},
	}, nil)

	r := newAuthedRouter(secret, handler.ListLiveMaterials, http.MethodGet, "/live-materials")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials?page_size=5", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestLiveMaterialHandler_Create_Success 验证创建接口成功返回素材。
func TestLiveMaterialHandler_Create_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		createFn: func(ctx context.Context, createdBy uint, name, liveURL, remark, ext string) (*model.LiveMaterial, error) {
			if createdBy != 5 {
				t.Errorf("createdBy = %d, want 5", createdBy)
			}
			return &model.LiveMaterial{
				ID:        1,
				Name:      name,
				LiveURL:   liveURL,
				URLType:   model.URLTypeFile,
				Remark:    remark,
				CreatedBy: createdBy,
				ASRStatus: model.ASRStatusPending,
			}, nil
		},
	}, accountsStub(&model.Account{ID: 5, Username: "admin", Nickname: "AdminNick"}))

	r := newAuthedRouter(secret, handler.CreateLiveMaterial, http.MethodPost, "/live-materials")
	token, err := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 5, Username: "admin"})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	body := []byte(`{"name":"测试素材","live_url":"https://example.com/live.mp4","remark":"备注"}`)
	req := httptest.NewRequest(http.MethodPost, "/live-materials", bytes.NewReader(body))
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
			Name      string `json:"name"`
			CreatedBy string `json:"created_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 {
		t.Errorf("code = %d, want 0", resp.Code)
	}
	if resp.Data.Name != "测试素材" {
		t.Errorf("data.name = %q, want 测试素材", resp.Data.Name)
	}
	if resp.Data.CreatedBy != "AdminNick" {
		t.Errorf("data.created_by = %q, want AdminNick", resp.Data.CreatedBy)
	}
}

// TestLiveMaterialHandler_Create_MissingRequired 验证缺少必填字段返回 400。
func TestLiveMaterialHandler_Create_MissingRequired(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{}, nil)
	r := newAuthedRouter(secret, handler.CreateLiveMaterial, http.MethodPost, "/live-materials")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodPost, "/live-materials", bytes.NewReader([]byte(`{"remark":"无名称和链接"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestLiveMaterialHandler_Create_ExistsReturns40901 验证素材已存在时返回 40901 及已有记录。
func TestLiveMaterialHandler_Create_ExistsReturns40901(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		createFn: func(ctx context.Context, createdBy uint, name, liveURL, remark, ext string) (*model.LiveMaterial, error) {
			return nil, &service.LiveMaterialExistsError{
				Material: &model.LiveMaterial{
					ID: 9, Name: "已有素材", LiveURL: "https://example.com/exist.mp4",
					URLType: model.URLTypeFile, ASRStatus: model.ASRStatusCompleted, CreatedBy: 2,
					LiveASR: "{}",
				},
				Cause: service.ErrLiveMaterialURLExists,
			}
		},
	}, accountsStub(&model.Account{ID: 2, Username: "u2", Nickname: "已有用户"}))

	r := newAuthedRouter(secret, handler.CreateLiveMaterial, http.MethodPost, "/live-materials")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})
	body := []byte(`{"name":"新名","live_url":"https://example.com/exist.mp4"}`)
	req := httptest.NewRequest(http.MethodPost, "/live-materials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Code != service.CodeLiveMaterialExists {
		t.Errorf("code = %d, want %d", resp.Code, service.CodeLiveMaterialExists)
	}
	if resp.Data.ID != 9 || resp.Data.Name != "已有素材" {
		t.Errorf("data = %+v, want existing material", resp.Data)
	}
}

// TestLiveMaterialHandler_Create_Unauthorized 验证未登录时返回 401。
func TestLiveMaterialHandler_Create_Unauthorized(t *testing.T) {
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{}, nil)
	r := gin.New()
	r.POST("/live-materials", handler.CreateLiveMaterial)

	body := []byte(`{"name":"测试","live_url":"https://example.com/a.mp4"}`)
	req := httptest.NewRequest(http.MethodPost, "/live-materials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestLiveMaterialHandler_Update_Success 验证编辑接口成功返回更新后的素材。
func TestLiveMaterialHandler_Update_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		updateFn: func(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error) {
			if id != 3 {
				t.Errorf("id = %d, want 3", id)
			}
			return &model.LiveMaterial{
				ID:      3,
				Name:    name,
				Remark:  remark,
				LiveURL: "https://example.com/unchanged.mp4",
			}, nil
		},
	}, nil)

	r := newAuthedRouter(secret, handler.UpdateLiveMaterial, http.MethodPut, "/live-materials/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	body := []byte(`{"name":"新名称","remark":"新备注"}`)
	req := httptest.NewRequest(http.MethodPut, "/live-materials/3", bytes.NewReader(body))
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
			Name    string `json:"name"`
			Remark  string `json:"remark"`
			LiveURL string `json:"live_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Data.Name != "新名称" || resp.Data.Remark != "新备注" {
		t.Errorf("unexpected data: %+v", resp.Data)
	}
	if resp.Data.LiveURL != "https://example.com/unchanged.mp4" {
		t.Errorf("live_url should remain unchanged, got %q", resp.Data.LiveURL)
	}
}

// TestLiveMaterialHandler_Update_NotFound 验证素材不存在时返回 404。
func TestLiveMaterialHandler_Update_NotFound(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		updateFn: func(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error) {
			return nil, service.ErrLiveMaterialNotFound
		},
	}, nil)
	r := newAuthedRouter(secret, handler.UpdateLiveMaterial, http.MethodPut, "/live-materials/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	body := []byte(`{"name":"名称","remark":""}`)
	req := httptest.NewRequest(http.MethodPut, "/live-materials/999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestLiveMaterialHandler_Update_InvalidID 验证非法 ID 返回 400。
func TestLiveMaterialHandler_Update_InvalidID(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{}, nil)
	r := newAuthedRouter(secret, handler.UpdateLiveMaterial, http.MethodPut, "/live-materials/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	body := []byte(`{"name":"名称","remark":""}`)
	req := httptest.NewRequest(http.MethodPut, "/live-materials/abc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestLiveMaterialHandler_List_WithFilters 验证列表筛选参数传递到 service。
func TestLiveMaterialHandler_List_WithFilters(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		listFn: func(ctx context.Context, page, pageSize int, opts service.LiveMaterialListOptions) ([]model.LiveMaterialListItem, int64, error) {
			if opts.Keywords != "游戏,周末" || opts.ASRKeywords != "发布会" {
				t.Errorf("unexpected opts: %+v", opts)
			}
			if opts.StartDate != "2026-01-01" || opts.EndDate != "2026-01-31" {
				t.Errorf("unexpected date opts: %+v", opts)
			}
			return nil, 0, nil
		},
	}, nil)
	r := newAuthedRouter(secret, handler.ListLiveMaterials, http.MethodGet, "/live-materials")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials?start_date=2026-01-01&end_date=2026-01-31&keywords=游戏,周末&asr_keywords=发布会", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestLiveMaterialHandler_List_InvalidPageSize 验证非法 page_size 返回 400。
func TestLiveMaterialHandler_List_InvalidPageSize(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{}, nil)
	r := newAuthedRouter(secret, handler.ListLiveMaterials, http.MethodGet, "/live-materials")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials?page_size=200", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestLiveMaterialHandler_Delete_Success 验证删除成功返回提示消息。
func TestLiveMaterialHandler_Delete_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		deleteFn: func(ctx context.Context, id uint) error {
			if id != 9 {
				t.Errorf("id = %d, want 9", id)
			}
			return nil
		},
	}, nil)
	r := newAuthedRouter(secret, handler.DeleteLiveMaterial, http.MethodDelete, "/live-materials/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodDelete, "/live-materials/9", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Message != "删除成功" {
		t.Errorf("message = %q, want 删除成功", resp.Message)
	}
}

// TestLiveMaterialHandler_Delete_NotFound 验证素材不存在时返回 404。
func TestLiveMaterialHandler_Delete_NotFound(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		deleteFn: func(ctx context.Context, id uint) error {
			return service.ErrLiveMaterialNotFound
		},
	}, nil)
	r := newAuthedRouter(secret, handler.DeleteLiveMaterial, http.MethodDelete, "/live-materials/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodDelete, "/live-materials/999", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// errLiveMaterialNotFound 模拟 service 返回的「不存在」错误。
func errLiveMaterialNotFound() error {
	return service.ErrLiveMaterialNotFound
}

// TestLiveMaterialHandler_DownloadASRSubtitle_Success 验证字幕接口直接返回 TXT 文件。
func TestLiveMaterialHandler_DownloadASRSubtitle_Success(t *testing.T) {
	secret := "handler-test-secret"
	txt := "关键词\n财富\n\n文字记录\n说话人1 00:02\n你好\n"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		downloadASRSubtitleFn: func(ctx context.Context, id uint) ([]byte, string, error) {
			if id != 12 {
				t.Errorf("id = %d, want 12", id)
			}
			return []byte(txt), "asr_subtitle_12.txt", nil
		},
	}, nil)

	r := newAuthedRouter(secret, handler.DownloadASRSubtitle, http.MethodGet, "/live-materials/:id/asr/subtitle")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials/12/asr/subtitle", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="asr_subtitle_12.txt"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", w.Header().Get("Content-Type"))
	}
	if w.Body.String() != txt {
		t.Errorf("body = %q, want %q", w.Body.String(), txt)
	}
}

// TestLiveMaterialHandler_DownloadASRSubtitle_NotReady 验证 ASR 未完成时返回 400。
func TestLiveMaterialHandler_DownloadASRSubtitle_NotReady(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		downloadASRSubtitleFn: func(ctx context.Context, id uint) ([]byte, string, error) {
			return nil, "", service.ErrASRSubtitleNotReady
		},
	}, nil)
	r := newAuthedRouter(secret, handler.DownloadASRSubtitle, http.MethodGet, "/live-materials/:id/asr/subtitle")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials/1/asr/subtitle", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestLiveMaterialHandler_DownloadASRSubtitle_NotFound 验证素材不存在时返回 404。
func TestLiveMaterialHandler_DownloadASRSubtitle_NotFound(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		downloadASRSubtitleFn: func(ctx context.Context, id uint) ([]byte, string, error) {
			return nil, "", service.ErrLiveMaterialNotFound
		},
	}, nil)
	r := newAuthedRouter(secret, handler.DownloadASRSubtitle, http.MethodGet, "/live-materials/:id/asr/subtitle")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/live-materials/999/asr/subtitle", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestLiveMaterialHandler_RetryASR_Success 验证仅 failed 可重试，且无需请求体。
func TestLiveMaterialHandler_RetryASR_Success(t *testing.T) {
	secret := "handler-test-secret"
	var gotID uint
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		retryASRFn: func(ctx context.Context, id uint) (*model.LiveMaterial, error) {
			gotID = id
			return &model.LiveMaterial{ID: id, ASRStatus: model.ASRStatusPending, ASRProgress: 0}, nil
		},
	}, nil)
	r := newAuthedRouter(secret, handler.RetryASR, http.MethodPost, "/live-materials/:id/asr/retry")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodPost, "/live-materials/7/asr/retry", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if gotID != 7 {
		t.Errorf("id = %d, want 7", gotID)
	}
}

func TestLiveMaterialHandler_RetryASR_OnlyFailed(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		retryASRFn: func(ctx context.Context, id uint) (*model.LiveMaterial, error) {
			return nil, service.ErrASRRetryOnlyFailed
		},
	}, nil)
	r := newAuthedRouter(secret, handler.RetryASR, http.MethodPost, "/live-materials/:id/asr/retry")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodPost, "/live-materials/1/asr/retry", bytes.NewBufferString(`{"force":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestLiveMaterialHandler_RetryASR_Processing(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		retryASRFn: func(ctx context.Context, id uint) (*model.LiveMaterial, error) {
			return nil, service.ErrASRAlreadyProcessing
		},
	}, nil)
	r := newAuthedRouter(secret, handler.RetryASR, http.MethodPost, "/live-materials/:id/asr/retry")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodPost, "/live-materials/1/asr/retry", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestLiveMaterialHandler_RetryASR_NotFound(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		retryASRFn: func(ctx context.Context, id uint) (*model.LiveMaterial, error) {
			return nil, service.ErrLiveMaterialNotFound
		},
	}, nil)
	r := newAuthedRouter(secret, handler.RetryASR, http.MethodPost, "/live-materials/:id/asr/retry")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodPost, "/live-materials/999/asr/retry", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
