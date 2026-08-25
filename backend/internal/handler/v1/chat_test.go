package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// mockChatService 用于 ChatHandler 单元测试。
type mockChatService struct {
	chatFn func(ctx context.Context, sysPrompt, usrPrompt string) (json.RawMessage, error)
}

func (m *mockChatService) Chat(ctx context.Context, sysPrompt, usrPrompt string) (json.RawMessage, error) {
	return m.chatFn(ctx, sysPrompt, usrPrompt)
}

func TestChatHandler_Chat_Success(t *testing.T) {
	handler := NewChatHandler(&mockChatService{
		chatFn: func(ctx context.Context, sysPrompt, usrPrompt string) (json.RawMessage, error) {
			if sysPrompt != "系统" || usrPrompt != "用户" {
				t.Errorf("sys=%q usr=%q", sysPrompt, usrPrompt)
			}
			return json.RawMessage(`{"id":"chatcmpl-1","choices":[{"message":{"content":"回复"}}]}`), nil
		},
	})

	r := gin.New()
	r.POST("/chat", handler.Chat)

	body := []byte(`{"sys_prompt":"系统","usr_prompt":"用户"}`)
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 || resp.Message != "success" {
		t.Errorf("code/message = %d/%s", resp.Code, resp.Message)
	}
	if resp.Data["id"] != "chatcmpl-1" {
		t.Errorf("data.id = %v, want chatcmpl-1", resp.Data["id"])
	}
}

// TestChatHandler_Chat_MissingUsrPrompt 验证缺少 usr_prompt 时返回 400。
func TestChatHandler_Chat_MissingUsrPrompt(t *testing.T) {
	handler := NewChatHandler(&mockChatService{})

	r := gin.New()
	r.POST("/chat", handler.Chat)

	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader([]byte(`{"sys_prompt":"x"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestChatHandler_Chat_ServiceError 验证服务层失败时返回 500。
func TestChatHandler_Chat_ServiceError(t *testing.T) {
	handler := NewChatHandler(&mockChatService{
		chatFn: func(ctx context.Context, sysPrompt, usrPrompt string) (json.RawMessage, error) {
			return nil, context.DeadlineExceeded
		},
	})

	r := gin.New()
	r.POST("/chat", handler.Chat)

	body := []byte(`{"usr_prompt":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// TestChatHandler_Chat_InvalidJSONResult 验证非法 JSON 结果时返回 500。
func TestChatHandler_Chat_InvalidJSONResult(t *testing.T) {
	handler := NewChatHandler(&mockChatService{
		chatFn: func(ctx context.Context, sysPrompt, usrPrompt string) (json.RawMessage, error) {
			return json.RawMessage(`not-json`), nil
		},
	})

	r := gin.New()
	r.POST("/chat", handler.Chat)

	body := []byte(`{"usr_prompt":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
