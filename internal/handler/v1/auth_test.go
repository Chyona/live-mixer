package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"live-mixer/internal/service"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockAuthService 用于 handler 单元测试。
type mockAuthService struct {
	loginFn func(ctx context.Context, username, password string) (*service.LoginResult, error)
}

func (m *mockAuthService) Login(ctx context.Context, username, password string) (*service.LoginResult, error) {
	return m.loginFn(ctx, username, password)
}

func TestAuthHandler_Login_Success(t *testing.T) {
	handler := NewAuthHandler(&mockAuthService{
		loginFn: func(ctx context.Context, username, password string) (*service.LoginResult, error) {
			return &service.LoginResult{
				Token:     "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test",
				ExpiresIn: 7200,
				ID:        "123",
				Username:  "admin",
				Nickname:  "张三",
				Avatar:    "https://cdn.example.com/avatar/123.jpg",
				Roles:     []string{"ADMIN"},
			}, nil
		},
	})

	r := gin.New()
	r.POST("/auth/login", handler.Login)

	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    service.LoginResult    `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 {
		t.Errorf("code = %d, want 0", resp.Code)
	}
	if resp.Data.Token == "" {
		t.Error("data.token should not be empty")
	}
	if resp.Data.ExpiresIn != 7200 {
		t.Errorf("data.expires_in = %d, want 7200", resp.Data.ExpiresIn)
	}
	if resp.Data.ID != "123" {
		t.Errorf("data.id = %q, want 123", resp.Data.ID)
	}
	if len(resp.Data.Roles) != 1 || resp.Data.Roles[0] != "ADMIN" {
		t.Errorf("data.roles = %v, want [ADMIN]", resp.Data.Roles)
	}
}

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	handler := NewAuthHandler(&mockAuthService{
		loginFn: func(ctx context.Context, username, password string) (*service.LoginResult, error) {
			return nil, context.DeadlineExceeded // 模拟失败，handler 只关心 err != nil
		},
	})

	r := gin.New()
	r.POST("/auth/login", handler.Login)

	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthHandler_Login_MissingFields(t *testing.T) {
	handler := NewAuthHandler(&mockAuthService{})

	r := gin.New()
	r.POST("/auth/login", handler.Login)

	body, _ := json.Marshal(map[string]string{"username": "admin"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
