package authjwt_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xiaonanln/goverse/goverseapi/authjwt"
)

var ctx = context.Background()

func signHS256(key []byte, sub string, roles []string, expiry time.Time) string {
	claims := jwt.MapClaims{"sub": sub, "exp": expiry.Unix()}
	if len(roles) > 0 {
		claims["roles"] = roles
	}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	return tok
}

func TestNew_HMACValidToken(t *testing.T) {
	key := []byte("supersecret")
	v := authjwt.New(authjwt.Options{SigningKey: key})

	token := signHS256(key, "alice", nil, time.Now().Add(time.Hour))
	id, err := v.Validate(ctx, map[string][]string{
		"authorization": {"Bearer " + token},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.UserID != "alice" {
		t.Errorf("want UserID=alice, got %q", id.UserID)
	}
}

func TestNew_HMACWithRoles(t *testing.T) {
	key := []byte("supersecret")
	v := authjwt.New(authjwt.Options{SigningKey: key})

	token := signHS256(key, "bob", []string{"admin", "editor"}, time.Now().Add(time.Hour))
	id, err := v.Validate(ctx, map[string][]string{
		"authorization": {"Bearer " + token},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.UserID != "bob" {
		t.Errorf("want UserID=bob, got %q", id.UserID)
	}
	if len(id.Roles) != 2 || id.Roles[0] != "admin" || id.Roles[1] != "editor" {
		t.Errorf("unexpected roles: %v", id.Roles)
	}
}

func TestNew_ExpiredToken(t *testing.T) {
	key := []byte("supersecret")
	v := authjwt.New(authjwt.Options{SigningKey: key})

	token := signHS256(key, "alice", nil, time.Now().Add(-time.Hour))
	_, err := v.Validate(ctx, map[string][]string{
		"authorization": {"Bearer " + token},
	})
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestNew_WrongSigningKey(t *testing.T) {
	key1 := []byte("key1")
	key2 := []byte("key2")
	v := authjwt.New(authjwt.Options{SigningKey: key2})

	token := signHS256(key1, "alice", nil, time.Now().Add(time.Hour))
	_, err := v.Validate(ctx, map[string][]string{
		"authorization": {"Bearer " + token},
	})
	if err == nil {
		t.Fatal("expected error for wrong signing key, got nil")
	}
}

func TestNew_MissingAuthorizationHeader(t *testing.T) {
	v := authjwt.New(authjwt.Options{SigningKey: []byte("k")})
	_, err := v.Validate(ctx, map[string][]string{})
	if err == nil {
		t.Fatal("expected error for missing header, got nil")
	}
}

func TestNew_IssuerMismatch(t *testing.T) {
	key := []byte("supersecret")
	v := authjwt.New(authjwt.Options{
		SigningKey: key,
		Issuer:     "https://expected.example.com",
	})

	claims := jwt.MapClaims{
		"sub": "alice",
		"iss": "https://other.example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	_, err := v.Validate(ctx, map[string][]string{
		"authorization": {"Bearer " + token},
	})
	if err == nil {
		t.Fatal("expected error for issuer mismatch, got nil")
	}
}

func TestNew_IssuerMatch(t *testing.T) {
	key := []byte("supersecret")
	issuer := "https://auth.example.com"
	v := authjwt.New(authjwt.Options{
		SigningKey: key,
		Issuer:     issuer,
	})

	claims := jwt.MapClaims{
		"sub": "carol",
		"iss": issuer,
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	id, err := v.Validate(ctx, map[string][]string{
		"authorization": {"Bearer " + token},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.UserID != "carol" {
		t.Errorf("want carol, got %q", id.UserID)
	}
}

func TestNew_ClockSkew(t *testing.T) {
	key := []byte("supersecret")
	skew := 30 * time.Second
	v := authjwt.New(authjwt.Options{SigningKey: key, ClockSkew: skew})

	// Token expired 10 seconds ago, within skew window
	token := signHS256(key, "alice", nil, time.Now().Add(-10*time.Second))
	id, err := v.Validate(ctx, map[string][]string{
		"authorization": {"Bearer " + token},
	})
	if err != nil {
		t.Fatalf("expected token within skew to pass: %v", err)
	}
	if id.UserID != "alice" {
		t.Errorf("want alice, got %q", id.UserID)
	}
}

func TestNew_RSAToken(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	v := authjwt.New(authjwt.Options{SigningKey: privKey.Public()})

	claims := jwt.MapClaims{"sub": "dave", "exp": time.Now().Add(time.Hour).Unix()}
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(privKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	id, err := v.Validate(ctx, map[string][]string{
		"authorization": {"Bearer " + token},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.UserID != "dave" {
		t.Errorf("want dave, got %q", id.UserID)
	}
}

func TestNew_ECDSAToken(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	v := authjwt.New(authjwt.Options{SigningKey: privKey.Public()})

	claims := jwt.MapClaims{"sub": "eve", "exp": time.Now().Add(time.Hour).Unix()}
	token, err := jwt.NewWithClaims(jwt.SigningMethodES256, claims).SignedString(privKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	id, err := v.Validate(ctx, map[string][]string{
		"authorization": {"Bearer " + token},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.UserID != "eve" {
		t.Errorf("want eve, got %q", id.UserID)
	}
}

func TestNew_UpperCaseAuthorizationHeader(t *testing.T) {
	key := []byte("k")
	v := authjwt.New(authjwt.Options{SigningKey: key})
	token := signHS256(key, "frank", nil, time.Now().Add(time.Hour))
	// HTTP sends "Authorization" (capital A)
	id, err := v.Validate(ctx, map[string][]string{
		"Authorization": {"Bearer " + token},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.UserID != "frank" {
		t.Errorf("want frank, got %q", id.UserID)
	}
}
