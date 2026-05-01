package goverseapi

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/util/callcontext"
)

func ctxWithIdentity(userID string, roles []string) context.Context {
	return callcontext.WithCallerIdentity(context.Background(), &callcontext.CallerIdentity{
		UserID: userID,
		Roles:  roles,
	})
}

func TestCallerUserID(t *testing.T) {
	ctx := ctxWithIdentity("alice", nil)
	if got := CallerUserID(ctx); got != "alice" {
		t.Errorf("got %q, want %q", got, "alice")
	}
}

func TestCallerUserID_Unauthenticated(t *testing.T) {
	if got := CallerUserID(context.Background()); got != "" {
		t.Errorf("expected empty string for unauthenticated context, got %q", got)
	}
}

func TestCallerRoles(t *testing.T) {
	ctx := ctxWithIdentity("alice", []string{"admin", "editor"})
	roles := CallerRoles(ctx)
	if len(roles) != 2 || roles[0] != "admin" || roles[1] != "editor" {
		t.Errorf("unexpected roles: %v", roles)
	}
}

func TestCallerRoles_Unauthenticated(t *testing.T) {
	if got := CallerRoles(context.Background()); got != nil {
		t.Errorf("expected nil roles for unauthenticated context, got %v", got)
	}
}

func TestCallerHasRole_Match(t *testing.T) {
	ctx := ctxWithIdentity("alice", []string{"admin", "editor"})
	if !CallerHasRole(ctx, "admin") {
		t.Error("expected CallerHasRole to return true for 'admin'")
	}
	if !CallerHasRole(ctx, "editor") {
		t.Error("expected CallerHasRole to return true for 'editor'")
	}
}

func TestCallerHasRole_NoMatch(t *testing.T) {
	ctx := ctxWithIdentity("alice", []string{"editor"})
	if CallerHasRole(ctx, "admin") {
		t.Error("expected CallerHasRole to return false for 'admin'")
	}
}

func TestCallerHasRole_Unauthenticated(t *testing.T) {
	if CallerHasRole(context.Background(), "admin") {
		t.Error("expected CallerHasRole to return false for unauthenticated context")
	}
}

func TestCallerHasRole_EmptyRoles(t *testing.T) {
	ctx := ctxWithIdentity("alice", []string{})
	if CallerHasRole(ctx, "admin") {
		t.Error("expected CallerHasRole to return false when roles list is empty")
	}
}
