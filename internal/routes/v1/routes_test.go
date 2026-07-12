package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	v1handler "live-mixer/internal/handler/v1"
	"live-mixer/internal/service"
	jwtpkg "live-mixer/pkg/jwt"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type routeMockAuthService struct{}

func (routeMockAuthService) Login(_ context.Context, username, password string) (*service.LoginResult, error) {
	return nil, errors.New("用户名或密码错误")
}

// TestRegisterRoutes_LoginPublicAndAccountsProtected 验证登录接口无需 JWT，账号接口需要 JWT。
func TestRegisterRoutes_LoginPublicAndAccountsProtected(t *testing.T) {
	secret := "route-test-secret"
	accountHandler := v1handler.NewAccountHandler(nil)
	authHandler := v1handler.NewAuthHandler(routeMockAuthService{})
	asrHandler := v1handler.NewASRHandler(nil)
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), accountHandler, authHandler, asrHandler, liveMaterialHandler, secret)

	// 未携带 Token 访问账号列表应被 JWT 中间件拦截
	req := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/accounts without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// 登录接口不应被 JWT 中间件拦截（缺少 body 时由 handler 返回 400）
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader([]byte(`{}`)))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)
	if loginW.Code == http.StatusUnauthorized {
		var resp map[string]interface{}
		_ = json.Unmarshal(loginW.Body.Bytes(), &resp)
		if msg, _ := resp["message"].(string); msg == "缺少 Authorization 请求头" {
			t.Error("POST /v1/auth/login should not require JWT")
		}
	}

	// 携带无效 Token 应被 JWT 中间件拦截
	badTokenReq := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
	badTokenReq.Header.Set("Authorization", "Bearer invalid.token")
	badTokenW := httptest.NewRecorder()
	r.ServeHTTP(badTokenW, badTokenReq)
	if badTokenW.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/accounts with invalid token: status = %d, want %d", badTokenW.Code, http.StatusUnauthorized)
	}

	// 携带有效 Token 应通过 JWT 中间件（后续 handler 可能因 nil service panic，用 Recovery 捕获）
	token, err := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{UserID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	rWithRecovery := gin.New()
	rWithRecovery.Use(gin.Recovery())
	RegisterRoutes(rWithRecovery.Group("/v1"), accountHandler, authHandler, asrHandler, liveMaterialHandler, secret)

	authReq := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
	authReq.Header.Set("Authorization", "Bearer "+token)
	authW := httptest.NewRecorder()
	rWithRecovery.ServeHTTP(authW, authReq)
	if authW.Code == http.StatusUnauthorized {
		t.Error("GET /v1/accounts with valid token should pass JWT middleware")
	}
}

// TestRegisterRoutes_ASRPublic 验证 ASR 接口无需 JWT 鉴权。
func TestRegisterRoutes_ASRPublic(t *testing.T) {
	secret := "route-test-secret"
	asrHandler := v1handler.NewASRHandler(&routeMockASRService{})

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, asrHandler, nil, secret)

	body := []byte(`{"audio_url":"https://example.com/test.wav"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/asr", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Error("POST /v1/asr should not require JWT")
	}
}

type routeMockASRService struct{}

func (routeMockASRService) Transcribe(_ context.Context, _ string) (json.RawMessage, error) {
	return json.RawMessage(`{"result":{"text":"ok"}}`), nil
}

// TestRegisterRoutes_LiveMaterialsProtected 验证直播素材写接口需要 JWT 鉴权。
func TestRegisterRoutes_LiveMaterialsProtected(t *testing.T) {
	secret := "route-test-secret"
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, liveMaterialHandler, secret)

	req := httptest.NewRequest(http.MethodPost, "/v1/live-materials", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("POST /v1/live-materials without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestRegisterRoutes_LiveMaterialsListProtected 验证直播素材列表接口需要 JWT 鉴权。
func TestRegisterRoutes_LiveMaterialsListProtected(t *testing.T) {
	secret := "route-test-secret"
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, liveMaterialHandler, secret)

	req := httptest.NewRequest(http.MethodGet, "/v1/live-materials", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/live-materials without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
