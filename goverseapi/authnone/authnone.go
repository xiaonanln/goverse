// Package authnone provides an AuthValidator that always succeeds with an
// empty CallerIdentity. Use it during local development when you want the
// gate auth plumbing to be exercised without requiring a real token provider.
//
// Example:
//
//	cfg := &gateserver.GateServerConfig{
//	    AuthValidator: authnone.New(),
//	}
package authnone

import (
	"context"

	"github.com/xiaonanln/goverse/util/callcontext"
)

type validator struct{}

// New returns an AuthValidator that accepts every connection and stamps an
// empty CallerIdentity onto it. CallerUserID(ctx) will return "" for all calls.
func New() *validator { return &validator{} }

func (v *validator) Validate(_ context.Context, _ map[string][]string) (*callcontext.CallerIdentity, error) {
	return &callcontext.CallerIdentity{}, nil
}
