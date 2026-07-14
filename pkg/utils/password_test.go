package utils

import (
	"strings"
	"testing"
	"unicode"
)

func TestComparePassword(t *testing.T) {
	hashed, err := HashPassword("admin")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !ComparePassword(hashed, "admin") {
		t.Error("ComparePassword() should match correct password")
	}
	if ComparePassword(hashed, "wrong") {
		t.Error("ComparePassword() should not match wrong password")
	}
}

// TestGenerateRandomPassword 验证随机密码长度、字符集与非法参数。
func TestGenerateRandomPassword(t *testing.T) {
	t.Run("default length", func(t *testing.T) {
		pwd, err := GenerateRandomPassword(DefaultRandomPasswordLength)
		if err != nil {
			t.Fatalf("GenerateRandomPassword() error = %v", err)
		}
		if len(pwd) != DefaultRandomPasswordLength {
			t.Fatalf("len = %d, want %d", len(pwd), DefaultRandomPasswordLength)
		}
		for _, r := range pwd {
			if !(unicode.IsDigit(r) || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
				t.Fatalf("非法字符 %q in %q", r, pwd)
			}
		}
	})

	t.Run("invalid length", func(t *testing.T) {
		if _, err := GenerateRandomPassword(0); err == nil {
			t.Fatal("expected error for length 0")
		}
		if _, err := GenerateRandomPassword(-1); err == nil {
			t.Fatal("expected error for negative length")
		}
	})

	t.Run("not constant", func(t *testing.T) {
		a, err := GenerateRandomPassword(16)
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		b, err := GenerateRandomPassword(16)
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if a == b {
			t.Fatalf("两次生成结果相同，疑似随机性异常: %q", a)
		}
		if strings.Trim(a, randomPasswordCharset) != "" {
			t.Fatalf("密码含非法字符: %q", a)
		}
	})
}

func TestParseRoles(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "admin", []string{"admin"}},
		{"multiple", "ADMIN,editor", []string{"ADMIN", "editor"}},
		{"with empty parts", "ADMIN,,editor", []string{"ADMIN", "editor"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRoles(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseRoles(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseRoles(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
