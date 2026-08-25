package model

import "testing"

func TestAccountDisplayName(t *testing.T) {
	if got := AccountDisplayName("alice", "爱丽丝"); got != "爱丽丝" {
		t.Errorf("prefer nickname: got %q", got)
	}
	if got := AccountDisplayName("alice", "  "); got != "alice" {
		t.Errorf("fallback username: got %q", got)
	}
	if got := AccountDisplayName("bob", ""); got != "bob" {
		t.Errorf("empty nickname: got %q", got)
	}
	acc := &Account{Username: "carol", Nickname: "小卡"}
	if got := acc.DisplayName(); got != "小卡" {
		t.Errorf("DisplayName: got %q", got)
	}
}
