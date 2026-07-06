package utils

import "testing"

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
