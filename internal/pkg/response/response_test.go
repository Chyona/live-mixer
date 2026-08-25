package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestForbidden 验证 Forbidden 返回 403 与统一响应结构。
func TestForbidden(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	Forbidden(c, "系统预置提示词不可修改")

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	var body Body
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Code != 403 {
		t.Errorf("code = %d, want 403", body.Code)
	}
	if body.Message != "系统预置提示词不可修改" {
		t.Errorf("message = %q", body.Message)
	}
}
