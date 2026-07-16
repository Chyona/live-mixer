package v1

import (
	"context"
	"testing"

	"live-mixer/internal/model"
)

func TestCreatedByResolver_NameOf(t *testing.T) {
	r := newCreatedByResolver(accountsStub(
		&model.Account{ID: 1, Username: "alice", Nickname: "AliceNick"},
		&model.Account{ID: 2, Username: "bob", Nickname: ""},
	))
	if got := r.nameOf(context.Background(), 1); got != "AliceNick" {
		t.Errorf("id=1: got %q", got)
	}
	if got := r.nameOf(context.Background(), 2); got != "bob" {
		t.Errorf("id=2: got %q", got)
	}
	if got := r.nameOf(context.Background(), 99); got != "" {
		t.Errorf("missing: got %q", got)
	}
}

func TestCreatedByResolver_NamesOf(t *testing.T) {
	r := newCreatedByResolver(accountsStub(
		&model.Account{ID: 1, Username: "alice", Nickname: "A"},
		&model.Account{ID: 2, Username: "bob", Nickname: ""},
	))
	got := r.namesOf(context.Background(), []uint{1, 2, 2, 0, 9})
	if got[1] != "A" || got[2] != "bob" {
		t.Errorf("got %#v", got)
	}
	if _, ok := got[9]; ok {
		t.Errorf("missing id should not appear: %#v", got)
	}
}
