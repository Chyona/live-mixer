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
	jwtpkg "live-mixer/pkg/jwt"

	"github.com/gin-gonic/gin"
)

// mockLiveMaterialService 用于 handler 单元测试。
type mockLiveMaterialService struct {
	createFn func(ctx context.Context, createdBy uint, name, liveURL, remark, ext string) (*model.LiveMaterial, error)
	updateFn func(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error)
	listFn   func(ctx context.Context, page, pageSize int) ([]model.LiveMaterialListItem, int64, error)
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

func (m *mockLiveMaterialService) List(ctx context.Context, page, pageSize int) ([]model.LiveMaterialListItem, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, page, pageSize)
	}
	return nil, 0, nil
}

// newAuthedRouter 创建带 JWT 鉴权的测试路由。
func newAuthedRouter(secret string, handler gin.HandlerFunc, method, path string) *gin.Engine {
	r := gin.New()
	r.Handle(method, path, middleware.JWTAuth(secret), handler)
	return r
}

// TestLiveMaterialHandler_List_Success 验证列表接口默认分页且不返回 live_asr。
func TestLiveMaterialHandler_List_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		listFn: func(ctx context.Context, page, pageSize int) ([]model.LiveMaterialListItem, int64, error) {
			if page != 1 || pageSize != 20 {
				t.Errorf("page/pageSize = %d/%d, want 1/20", page, pageSize)
			}
			return []model.LiveMaterialListItem{
				{
					ID:        1,
					Name:      "素材A",
					LiveURL:   "https://example.com/a.mp4",
					ASRStatus: model.ASRStatusCompleted,
				},
			}, 1, nil
		},
	})

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
			List     []model.LiveMaterialListItem `json:"list"`
			Total    int64                        `json:"total"`
			Page     int                          `json:"page"`
			PageSize int                          `json:"page_size"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Data.PageSize != 20 {
		t.Errorf("page_size = %d, want 20", resp.Data.PageSize)
	}
	if len(resp.Data.List) != 1 {
		t.Fatalf("list len = %d, want 1", len(resp.Data.List))
	}
	if resp.Data.List[0].Name != "素材A" {
		t.Errorf("name = %q, want 素材A", resp.Data.List[0].Name)
	}
	if strings.Contains(w.Body.String(), `"live_asr"`) {
		t.Error("response should not contain live_asr field")
	}
}

// TestLiveMaterialHandler_List_CustomPageSize 验证自定义 page_size 生效。
func TestLiveMaterialHandler_List_CustomPageSize(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{
		listFn: func(ctx context.Context, page, pageSize int) ([]model.LiveMaterialListItem, int64, error) {
			if pageSize != 5 {
				t.Errorf("pageSize = %d, want 5", pageSize)
			}
			return nil, 0, nil
		},
	})

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
				Remark:    remark,
				CreatedBy: createdBy,
				ASRStatus: model.ASRStatusPending,
			}, nil
		},
	})

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
		Code int                 `json:"code"`
		Data model.LiveMaterial `json:"data"`
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
	if resp.Data.CreatedBy != 5 {
		t.Errorf("data.created_by = %d, want 5", resp.Data.CreatedBy)
	}
}

// TestLiveMaterialHandler_Create_MissingRequired 验证缺少必填字段返回 400。
func TestLiveMaterialHandler_Create_MissingRequired(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{})
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

// TestLiveMaterialHandler_Create_Unauthorized 验证未登录时返回 401。
func TestLiveMaterialHandler_Create_Unauthorized(t *testing.T) {
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{})
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
	})

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
		Code int                 `json:"code"`
		Data model.LiveMaterial `json:"data"`
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
			return nil, errLiveMaterialNotFound()
		},
	})
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
	handler := NewLiveMaterialHandler(&mockLiveMaterialService{})
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

// errLiveMaterialNotFound 模拟 service 返回的「不存在」错误。
func errLiveMaterialNotFound() error {
	return &liveMaterialNotFoundError{}
}

type liveMaterialNotFoundError struct{}

func (e *liveMaterialNotFoundError) Error() string { return "直播素材不存在" }
