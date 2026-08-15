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
	"live-mixer/internal/pkg/asr"
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
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil, nil)
	llmSystemPromptHandler := v1handler.NewLLMSystemPromptHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), accountHandler, authHandler, asrHandler, liveMaterialHandler, llmSystemPromptHandler, nil, nil, nil, secret)

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
	RegisterRoutes(rWithRecovery.Group("/v1"), accountHandler, authHandler, asrHandler, liveMaterialHandler, llmSystemPromptHandler, nil, nil, nil, secret)

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
	RegisterRoutes(r.Group("/v1"), nil, nil, asrHandler, nil, nil, nil, nil, nil, secret)

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

func (routeMockASRService) TranscribeWithProgress(_ context.Context, _ string, _ asr.ProgressCallback) (json.RawMessage, error) {
	return json.RawMessage(`{"result":{"text":"ok"}}`), nil
}

// routeMockChatService 路由测试用的对话服务 mock。
type routeMockChatService struct{}

func (routeMockChatService) Chat(_ context.Context, _, _ string) (json.RawMessage, error) {
	return json.RawMessage(`{"id":"ok","choices":[]}`), nil
}

// TestRegisterRoutes_ChatPublic 验证大模型同步对话接口无需 JWT 鉴权。
func TestRegisterRoutes_ChatPublic(t *testing.T) {
	secret := "route-test-secret"
	chatHandler := v1handler.NewChatHandler(routeMockChatService{})

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, nil, nil, nil, nil, chatHandler, secret)

	body := []byte(`{"usr_prompt":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Error("POST /v1/chat should not require JWT")
	}
}

// TestRegisterRoutes_LiveMaterialsProtected 验证直播素材写接口需要 JWT 鉴权。
func TestRegisterRoutes_LiveMaterialsProtected(t *testing.T) {
	secret := "route-test-secret"
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, liveMaterialHandler, nil, nil, nil, nil, secret)

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
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, liveMaterialHandler, nil, nil, nil, nil, secret)

	req := httptest.NewRequest(http.MethodGet, "/v1/live-materials", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/live-materials without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestRegisterRoutes_LiveMaterialsGetByIDProtected 验证直播素材详情接口需要 JWT 鉴权。
func TestRegisterRoutes_LiveMaterialsGetByIDProtected(t *testing.T) {
	secret := "route-test-secret"
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, liveMaterialHandler, nil, nil, nil, nil, secret)

	req := httptest.NewRequest(http.MethodGet, "/v1/live-materials/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/live-materials/1 without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestRegisterRoutes_LiveMaterialsDeleteProtected 验证直播素材删除接口需要 JWT 鉴权。
func TestRegisterRoutes_LiveMaterialsDeleteProtected(t *testing.T) {
	secret := "route-test-secret"
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, liveMaterialHandler, nil, nil, nil, nil, secret)

	req := httptest.NewRequest(http.MethodDelete, "/v1/live-materials/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("DELETE /v1/live-materials/1 without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestRegisterRoutes_LLMSystemPromptsProtected 验证系统提示词接口需要 JWT 鉴权。
func TestRegisterRoutes_LLMSystemPromptsProtected(t *testing.T) {
	secret := "route-test-secret"
	llmHandler := v1handler.NewLLMSystemPromptHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, nil, llmHandler, nil, nil, nil, secret)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/llm-system-prompts"},
		{http.MethodPost, "/v1/llm-system-prompts"},
		{http.MethodGet, "/v1/llm-system-prompts/1"},
		{http.MethodPut, "/v1/llm-system-prompts/1"},
		{http.MethodDelete, "/v1/llm-system-prompts/1"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.method == http.MethodPost || tc.method == http.MethodPut {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: status = %d, want %d", tc.method, tc.path, w.Code, http.StatusUnauthorized)
		}
	}
}

// TestRegisterRoutes_VideoProjectsProtected 验证剪辑项目接口需要 JWT 鉴权。
func TestRegisterRoutes_VideoProjectsProtected(t *testing.T) {
	secret := "route-test-secret"
	videoHandler := v1handler.NewVideoProjectHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, nil, nil, videoHandler, nil, nil, secret)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/video-projects"},
		{http.MethodPost, "/v1/video-projects"},
		{http.MethodGet, "/v1/video-projects/1"},
		{http.MethodPut, "/v1/video-projects/1"},
		{http.MethodDelete, "/v1/video-projects/1"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.method == http.MethodPost || tc.method == http.MethodPut {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: status = %d, want %d", tc.method, tc.path, w.Code, http.StatusUnauthorized)
		}
	}
}

// TestRegisterRoutes_TasksProtected 验证异步任务接口需要 JWT 鉴权。
func TestRegisterRoutes_TasksProtected(t *testing.T) {
	secret := "route-test-secret"
	taskHandler := v1handler.NewTaskHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, nil, nil, nil, taskHandler, nil, secret)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/tasks"},
		{http.MethodPost, "/v1/tasks/ai-slice"},
		{http.MethodPost, "/v1/tasks/draft"},
		{http.MethodPost, "/v1/tasks/ai-slice-draft"},
		{http.MethodGet, "/v1/tasks/11111111-1111-1111-1111-111111111111"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: status = %d, want %d", tc.method, tc.path, w.Code, http.StatusUnauthorized)
		}
	}
}

// TestRegisterRoutes_ASRRetryProtected 验证重新 ASR 接口需要 JWT 鉴权。
func TestRegisterRoutes_ASRRetryProtected(t *testing.T) {
	secret := "route-test-secret"
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, liveMaterialHandler, nil, nil, nil, nil, secret)

	req := httptest.NewRequest(http.MethodPost, "/v1/live-materials/1/asr/retry", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("POST /v1/live-materials/1/asr/retry without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestRegisterRoutes_ASRSubtitleProtected 验证 ASR 字幕下载接口需要 JWT 鉴权。
func TestRegisterRoutes_ASRSubtitleProtected(t *testing.T) {
	secret := "route-test-secret"
	liveMaterialHandler := v1handler.NewLiveMaterialHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, liveMaterialHandler, nil, nil, nil, nil, secret)

	req := httptest.NewRequest(http.MethodGet, "/v1/live-materials/1/asr/subtitle", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/live-materials/1/asr/subtitle without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestRegisterRoutes_LiveMaterialVideoProjectsProtected 验证素材关联项目列表接口需要 JWT 鉴权。
func TestRegisterRoutes_LiveMaterialVideoProjectsProtected(t *testing.T) {
	secret := "route-test-secret"
	videoHandler := v1handler.NewVideoProjectHandler(nil, nil)

	r := gin.New()
	RegisterRoutes(r.Group("/v1"), nil, nil, nil, nil, nil, videoHandler, nil, nil, secret)

	req := httptest.NewRequest(http.MethodGet, "/v1/live-materials/1/video-projects", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/live-materials/1/video-projects without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
