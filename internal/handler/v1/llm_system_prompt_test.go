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

	"github.com/gin-gonic/gin"
)

// mockLLMSystemPromptService 用于 handler 单元测试。
type mockLLMSystemPromptService struct {
	createFn func(ctx context.Context, createdBy uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error)
	updateFn func(ctx context.Context, id uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error)
	deleteFn func(ctx context.Context, id uint) error
	listFn   func(ctx context.Context, page, pageSize int, opts service.LLMSystemPromptListOptions) ([]model.LLMSystemPromptListItem, int64, error)
	getFn    func(ctx context.Context, id uint) (*model.LLMSystemPrompt, error)
}

func (m *mockLLMSystemPromptService) Create(ctx context.Context, createdBy uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error) {
	if m.createFn != nil {
		return m.createFn(ctx, createdBy, name, content, remark, ext)
	}
	return nil, nil
}

func (m *mockLLMSystemPromptService) Update(ctx context.Context, id uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, name, content, remark, ext)
	}
	return nil, nil
}

func (m *mockLLMSystemPromptService) Delete(ctx context.Context, id uint) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockLLMSystemPromptService) List(ctx context.Context, page, pageSize int, opts service.LLMSystemPromptListOptions) ([]model.LLMSystemPromptListItem, int64, error) {
	if m.listFn != nil {
		return m.listFn(ctx, page, pageSize, opts)
	}
	return nil, 0, nil
}

func (m *mockLLMSystemPromptService) Get(ctx context.Context, id uint) (*model.LLMSystemPrompt, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return nil, nil
}

// TestLLMSystemPromptHandler_List_Success 验证列表接口默认分页与筛选参数传递。
func TestLLMSystemPromptHandler_List_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLLMSystemPromptHandler(&mockLLMSystemPromptService{
		listFn: func(ctx context.Context, page, pageSize int, opts service.LLMSystemPromptListOptions) ([]model.LLMSystemPromptListItem, int64, error) {
			if page != 1 || pageSize != 10 {
				t.Errorf("page/pageSize = %d/%d, want 1/10", page, pageSize)
			}
			if opts.Keywords != "直播,话术" || opts.StartDate != "2026-01-01" || opts.EndDate != "2026-01-31" {
				t.Errorf("unexpected opts: %+v", opts)
			}
			return []model.LLMSystemPromptListItem{
				{ID: 1, Name: "直播话术", Content: "完整提示词内容", IsEditable: 1},
			}, 1, nil
		},
	}, nil)

	r := newAuthedRouter(secret, handler.ListLLMSystemPrompts, http.MethodGet, "/llm-system-prompts")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/llm-system-prompts?keywords=直播,话术&start_date=2026-01-01&end_date=2026-01-31", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestLLMSystemPromptHandler_List_InvalidPageSize 验证非法 page_size 返回 400。
func TestLLMSystemPromptHandler_List_InvalidPageSize(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLLMSystemPromptHandler(&mockLLMSystemPromptService{}, nil)
	r := newAuthedRouter(secret, handler.ListLLMSystemPrompts, http.MethodGet, "/llm-system-prompts")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/llm-system-prompts?page_size=200", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestLLMSystemPromptHandler_Create_Success 验证创建接口成功返回提示词。
func TestLLMSystemPromptHandler_Create_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLLMSystemPromptHandler(&mockLLMSystemPromptService{
		createFn: func(ctx context.Context, createdBy uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error) {
			if createdBy != 8 {
				t.Errorf("createdBy = %d, want 8", createdBy)
			}
			return &model.LLMSystemPrompt{ID: 1, Name: name, Content: content, CreatedBy: createdBy, IsEditable: 1}, nil
		},
	}, nil)

	r := newAuthedRouter(secret, handler.CreateLLMSystemPrompt, http.MethodPost, "/llm-system-prompts")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 8, Username: "admin"})

	body := []byte(`{"name":"商品介绍","content":"你是文案专家","remark":"备注"}`)
	req := httptest.NewRequest(http.MethodPost, "/llm-system-prompts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestLLMSystemPromptHandler_Create_Unauthorized 验证未登录时返回 401。
func TestLLMSystemPromptHandler_Create_Unauthorized(t *testing.T) {
	handler := NewLLMSystemPromptHandler(&mockLLMSystemPromptService{}, nil)
	r := gin.New()
	r.POST("/llm-system-prompts", handler.CreateLLMSystemPrompt)

	body := []byte(`{"name":"测试","content":"内容"}`)
	req := httptest.NewRequest(http.MethodPost, "/llm-system-prompts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestLLMSystemPromptHandler_Get_NotFound 验证详情不存在时返回 404。
func TestLLMSystemPromptHandler_Get_NotFound(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLLMSystemPromptHandler(&mockLLMSystemPromptService{
		getFn: func(ctx context.Context, id uint) (*model.LLMSystemPrompt, error) {
			return nil, service.ErrLLMSystemPromptNotFound
		},
	}, nil)
	r := newAuthedRouter(secret, handler.GetLLMSystemPrompt, http.MethodGet, "/llm-system-prompts/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodGet, "/llm-system-prompts/99", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestLLMSystemPromptHandler_Update_Forbidden 验证预置提示词更新返回 403。
func TestLLMSystemPromptHandler_Update_Forbidden(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLLMSystemPromptHandler(&mockLLMSystemPromptService{
		updateFn: func(ctx context.Context, id uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error) {
			return nil, service.ErrLLMSystemPromptNotEditable
		},
	}, nil)
	r := newAuthedRouter(secret, handler.UpdateLLMSystemPrompt, http.MethodPut, "/llm-system-prompts/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	body := []byte(`{"name":"名称","content":"内容"}`)
	req := httptest.NewRequest(http.MethodPut, "/llm-system-prompts/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// TestLLMSystemPromptHandler_Delete_Success 验证删除成功返回提示消息。
func TestLLMSystemPromptHandler_Delete_Success(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLLMSystemPromptHandler(&mockLLMSystemPromptService{
		deleteFn: func(ctx context.Context, id uint) error {
			if id != 5 {
				t.Errorf("id = %d, want 5", id)
			}
			return nil
		},
	}, nil)
	r := newAuthedRouter(secret, handler.DeleteLLMSystemPrompt, http.MethodDelete, "/llm-system-prompts/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodDelete, "/llm-system-prompts/5", nil)
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

// TestLLMSystemPromptHandler_Delete_Forbidden 验证预置提示词删除返回 403。
func TestLLMSystemPromptHandler_Delete_Forbidden(t *testing.T) {
	secret := "handler-test-secret"
	handler := NewLLMSystemPromptHandler(&mockLLMSystemPromptService{
		deleteFn: func(ctx context.Context, id uint) error {
			return service.ErrLLMSystemPromptNotDeletable
		},
	}, nil)
	r := newAuthedRouter(secret, handler.DeleteLLMSystemPrompt, http.MethodDelete, "/llm-system-prompts/:id")
	token, _ := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})

	req := httptest.NewRequest(http.MethodDelete, "/llm-system-prompts/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}
