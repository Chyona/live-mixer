package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	jwtpkg "live-mixer/pkg/jwt"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestJWTAuth_ValidToken(t *testing.T) {
	secret := "test-secret"
	token, err := jwtpkg.GenerateToken(secret, 7200, jwtpkg.UserClaims{
		UserID:   123,
		Username: "admin",
		Nickname: "张三",
		Avatar:   "https://cdn.example.com/avatar/123.jpg",
		Roles:    []string{"ADMIN"},
	})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	r := gin.New()
	r.GET("/protected", JWTAuth(secret), func(c *gin.Context) {
		user, ok := GetAuthUser(c)
		if !ok {
			t.Error("GetAuthUser() should return true")
		}
		if user.ID != 123 || user.Username != "admin" {
			t.Errorf("unexpected user: %+v", user)
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestJWTAuth_MissingHeader(t *testing.T) {
	r := gin.New()
	r.GET("/protected", JWTAuth("test-secret"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestJWTAuth_InvalidToken(t *testing.T) {
	r := gin.New()
	r.GET("/protected", JWTAuth("test-secret"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid.token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestJWTAuth_MalformedHeader(t *testing.T) {
	r := gin.New()
	r.GET("/protected", JWTAuth("test-secret"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "InvalidFormat")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
