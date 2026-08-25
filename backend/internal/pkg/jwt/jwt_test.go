package jwt

import (
	"testing"
	"time"
)

func TestGenerateAndParseToken(t *testing.T) {
	secret := "test-secret"
	expiresIn := 7200
	claims := UserClaims{
		UserID:   123,
		Username: "admin",
		Nickname: "张三",
		Avatar:   "https://cdn.example.com/avatar/123.jpg",
		Roles:    []string{"ADMIN"},
	}

	token, err := GenerateToken(secret, expiresIn, claims)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken() returned empty token")
	}

	parsed, err := ParseToken(secret, token)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}

	if parsed.UserID != claims.UserID {
		t.Errorf("UserID = %d, want %d", parsed.UserID, claims.UserID)
	}
	if parsed.Username != claims.Username {
		t.Errorf("Username = %q, want %q", parsed.Username, claims.Username)
	}
	if parsed.Nickname != claims.Nickname {
		t.Errorf("Nickname = %q, want %q", parsed.Nickname, claims.Nickname)
	}
	if parsed.Avatar != claims.Avatar {
		t.Errorf("Avatar = %q, want %q", parsed.Avatar, claims.Avatar)
	}
	if len(parsed.Roles) != 1 || parsed.Roles[0] != "ADMIN" {
		t.Errorf("Roles = %v, want [ADMIN]", parsed.Roles)
	}
}

func TestGenerateToken_EmptySecret(t *testing.T) {
	_, err := GenerateToken("", 7200, UserClaims{UserID: 1})
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestParseToken_InvalidToken(t *testing.T) {
	_, err := ParseToken("test-secret", "invalid.token.value")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestParseToken_WrongSecret(t *testing.T) {
	token, err := GenerateToken("secret-a", 7200, UserClaims{UserID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	_, err = ParseToken("secret-b", token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestParseToken_ExpiredToken(t *testing.T) {
	secret := "test-secret"
	claims := UserClaims{
		UserID:   1,
		Username: "admin",
	}
	// 使用 1 秒过期
	token, err := GenerateToken(secret, 1, claims)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	time.Sleep(2 * time.Second)

	_, err = ParseToken(secret, token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}
