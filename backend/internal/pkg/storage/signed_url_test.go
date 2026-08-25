package storage

import (
	"testing"
	"time"
)

func TestResolveSignedURLExpireDays(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, DefaultSignedURLExpireDays},
		{-1, DefaultSignedURLExpireDays},
		{7, 7},
		{30, 30},
	}
	for _, tt := range tests {
		if got := ResolveSignedURLExpireDays(tt.input); got != tt.want {
			t.Errorf("ResolveSignedURLExpireDays(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestSignedURLExpireDuration(t *testing.T) {
	got := signedURLExpireDuration(30, ProviderCOS)
	want := 30 * 24 * time.Hour
	if got != want {
		t.Errorf("COS duration = %v, want %v", got, want)
	}

	gotTOS := signedURLExpireDuration(30, ProviderTOS)
	wantTOS := time.Duration(tosSignedURLMaxExpireSeconds) * time.Second
	if gotTOS != wantTOS {
		t.Errorf("TOS duration = %v, want %v (7 day cap)", gotTOS, wantTOS)
	}
}
