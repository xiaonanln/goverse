// Package authjwt provides an AuthValidator that verifies JWT bearer tokens.
//
// Supported algorithms: HS256, HS384, HS512 (HMAC) and RS256, RS384, RS512
// (RSA) and ES256, ES384, ES512 (ECDSA). The token is read from the
// "authorization" key in the headers map (gRPC metadata convention) or the
// "Authorization" key (HTTP header convention); both are checked.
//
// The JWT "sub" claim is mapped to CallerIdentity.UserID. If the token
// contains a "roles" claim (array of strings), those values are mapped to
// CallerIdentity.Roles.
//
// Example — HMAC shared secret:
//
//	cfg := &gateserver.GateServerConfig{
//	    AuthValidator: authjwt.New(authjwt.Options{
//	        SigningKey: []byte("my-secret"),
//	    }),
//	}
//
// Example — RSA public key:
//
//	pubKey, _ := jwt.ParseRSAPublicKeyFromPEM(pemBytes)
//	cfg := &gateserver.GateServerConfig{
//	    AuthValidator: authjwt.New(authjwt.Options{
//	        SigningKey: pubKey,
//	        Issuer:    "https://auth.example.com",
//	        Audience:  []string{"my-service"},
//	    }),
//	}
package authjwt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xiaonanln/goverse/util/callcontext"
)

// Options configure the JWT validator.
type Options struct {
	// SigningKey is the key used to verify the token signature.
	// For HMAC algorithms (HS256/HS384/HS512) use []byte.
	// For RSA algorithms (RS256/RS384/RS512) use *rsa.PublicKey.
	// For ECDSA algorithms (ES256/ES384/ES512) use *ecdsa.PublicKey.
	// Required.
	SigningKey interface{}

	// Issuer, if non-empty, requires the token's "iss" claim to match.
	Issuer string

	// Audience, if non-empty, requires the token's "aud" claim to include
	// at least one of the listed values.
	Audience []string

	// ClockSkew is the maximum allowed difference between the token's
	// expiry and the current time. Defaults to 0 (no skew allowed).
	ClockSkew time.Duration
}

type validator struct {
	opts Options
}

// New returns an AuthValidator that verifies JWT bearer tokens using opts.
func New(opts Options) *validator {
	return &validator{opts: opts}
}

// goverseClaims extends RegisteredClaims with an optional "roles" array.
type goverseClaims struct {
	Roles []string `json:"roles,omitempty"`
	jwt.RegisteredClaims
}

// Validate implements callcontext.AuthValidator.
//
// headers is the union of request headers (HTTP) or gRPC metadata (lowercase
// keys). The bearer token is read from the "authorization" key.
func (v *validator) Validate(_ context.Context, headers map[string][]string) (*callcontext.CallerIdentity, error) {
	raw := bearerToken(headers)
	if raw == "" {
		return nil, fmt.Errorf("missing or empty Authorization header")
	}

	parserOpts := []jwt.ParserOption{
		jwt.WithLeeway(v.opts.ClockSkew),
	}
	if v.opts.Issuer != "" {
		parserOpts = append(parserOpts, jwt.WithIssuer(v.opts.Issuer))
	}
	if len(v.opts.Audience) > 0 {
		for _, aud := range v.opts.Audience {
			parserOpts = append(parserOpts, jwt.WithAudience(aud))
		}
	}

	claims := &goverseClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (interface{}, error) {
		return v.opts.SigningKey, nil
	}, parserOpts...)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return nil, fmt.Errorf("token missing sub claim")
	}

	return &callcontext.CallerIdentity{
		UserID: sub,
		Roles:  claims.Roles,
	}, nil
}

// bearerToken extracts the raw JWT from headers["authorization"] or
// headers["Authorization"], stripping the "Bearer " prefix.
func bearerToken(headers map[string][]string) string {
	for _, key := range []string{"authorization", "Authorization"} {
		vals := headers[key]
		if len(vals) > 0 && vals[0] != "" {
			return strings.TrimPrefix(vals[0], "Bearer ")
		}
	}
	return ""
}
