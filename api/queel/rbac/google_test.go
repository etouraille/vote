package rbac

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// testGoogleToken builds a real RS256-signed token shaped like a Google ID
// token, signed by a throwaway key pair — so verifyGoogleIDToken's actual
// signature-checking code runs for real, with a fake key source standing
// in only for the network fetch of Google's own JWKS.
func testGoogleToken(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()

	header := map[string]string{"alg": "RS256", "kid": kid}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestVerifyGoogleIDTokenAcceptsAValidToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	lookupKey := func(kid string) (*rsa.PublicKey, error) {
		if kid != "test-kid" {
			return nil, errors.New("unknown kid")
		}
		return &key.PublicKey, nil
	}

	token := testGoogleToken(t, key, "test-kid", map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            "my-client-id",
		"sub":            "1234567890",
		"email":          "Alice@Example.com",
		"email_verified": true,
		"exp":            time.Now().Add(time.Hour).Unix(),
	})

	identity, err := verifyGoogleIDToken(token, "my-client-id", lookupKey)
	if err != nil {
		t.Fatalf("expected valid token, got: %v", err)
	}
	if identity.Subject != "1234567890" {
		t.Errorf("expected subject 1234567890, got %q", identity.Subject)
	}
	if identity.Email != "alice@example.com" {
		t.Errorf("expected lowercased email, got %q", identity.Email)
	}
	if !identity.EmailVerified {
		t.Error("expected EmailVerified true")
	}
}

func TestVerifyGoogleIDTokenAcceptsStringEmailVerified(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	lookupKey := func(string) (*rsa.PublicKey, error) { return &key.PublicKey, nil }

	token := testGoogleToken(t, key, "kid", map[string]any{
		"iss": "accounts.google.com", "aud": "client", "sub": "s", "email": "e@x.com",
		"email_verified": "true", "exp": time.Now().Add(time.Hour).Unix(),
	})

	identity, err := verifyGoogleIDToken(token, "client", lookupKey)
	if err != nil {
		t.Fatal(err)
	}
	if !identity.EmailVerified {
		t.Error("expected EmailVerified true from string \"true\"")
	}
}

func TestVerifyGoogleIDTokenRejectsWrongAudience(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	lookupKey := func(string) (*rsa.PublicKey, error) { return &key.PublicKey, nil }

	token := testGoogleToken(t, key, "kid", map[string]any{
		"iss": "https://accounts.google.com", "aud": "someone-elses-client-id", "sub": "s",
		"email": "e@x.com", "email_verified": true, "exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := verifyGoogleIDToken(token, "my-client-id", lookupKey); !errors.Is(err, ErrGoogleTokenInvalid) {
		t.Fatalf("expected ErrGoogleTokenInvalid, got %v", err)
	}
}

func TestVerifyGoogleIDTokenRejectsExpiredToken(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	lookupKey := func(string) (*rsa.PublicKey, error) { return &key.PublicKey, nil }

	token := testGoogleToken(t, key, "kid", map[string]any{
		"iss": "https://accounts.google.com", "aud": "client", "sub": "s",
		"email": "e@x.com", "email_verified": true, "exp": time.Now().Add(-time.Hour).Unix(),
	})

	if _, err := verifyGoogleIDToken(token, "client", lookupKey); !errors.Is(err, ErrGoogleTokenExpired) {
		t.Fatalf("expected ErrGoogleTokenExpired, got %v", err)
	}
}

func TestVerifyGoogleIDTokenRejectsWrongIssuer(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	lookupKey := func(string) (*rsa.PublicKey, error) { return &key.PublicKey, nil }

	token := testGoogleToken(t, key, "kid", map[string]any{
		"iss": "https://evil.example.com", "aud": "client", "sub": "s",
		"email": "e@x.com", "email_verified": true, "exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := verifyGoogleIDToken(token, "client", lookupKey); !errors.Is(err, ErrGoogleTokenInvalid) {
		t.Fatalf("expected ErrGoogleTokenInvalid, got %v", err)
	}
}

func TestVerifyGoogleIDTokenRejectsBadSignature(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	// Signed by otherKey, but verification will be attempted against key's
	// public half — simulating a forged/tampered token.
	lookupKey := func(string) (*rsa.PublicKey, error) { return &key.PublicKey, nil }

	token := testGoogleToken(t, otherKey, "kid", map[string]any{
		"iss": "https://accounts.google.com", "aud": "client", "sub": "s",
		"email": "e@x.com", "email_verified": true, "exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := verifyGoogleIDToken(token, "client", lookupKey); !errors.Is(err, ErrGoogleTokenInvalid) {
		t.Fatalf("expected ErrGoogleTokenInvalid, got %v", err)
	}
}

func TestVerifyGoogleIDTokenRejectsMalformedToken(t *testing.T) {
	lookupKey := func(string) (*rsa.PublicKey, error) { t.Fatal("should not be called"); return nil, nil }

	if _, err := verifyGoogleIDToken("not-a-jwt", "client", lookupKey); !errors.Is(err, ErrGoogleTokenInvalid) {
		t.Fatalf("expected ErrGoogleTokenInvalid, got %v", err)
	}
}

func TestVerifyGoogleIDTokenRejectsEmptyClientID(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	lookupKey := func(string) (*rsa.PublicKey, error) { return &key.PublicKey, nil }

	token := testGoogleToken(t, key, "kid", map[string]any{
		"iss": "https://accounts.google.com", "aud": "client", "sub": "s",
		"email": "e@x.com", "email_verified": true, "exp": time.Now().Add(time.Hour).Unix(),
	})

	if _, err := verifyGoogleIDToken(token, "", lookupKey); !errors.Is(err, ErrGoogleTokenInvalid) {
		t.Fatalf("expected ErrGoogleTokenInvalid for empty configured client ID, got %v", err)
	}
}

// TestVerifyGoogleIDTokenEndToEnd exercises the public VerifyGoogleIDToken
// entry point — including fetchGoogleJWKS/rsaPublicKeyFromJWK, not just the
// injected-lookup unit tests above — against a fake HTTP server standing in
// for Google's real JWKS endpoint.
func TestVerifyGoogleIDTokenEndToEnd(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwk := map[string]string{
			"kid": "e2e-kid",
			"kty": "RSA",
			"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}), // 65537
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{jwk}})
	}))
	defer server.Close()

	originalURL := googleJWKSURL
	googleJWKSURL = server.URL
	googleKeys = &googleJWKSCache{} // fresh cache so the fake server actually gets hit
	defer func() {
		googleJWKSURL = originalURL
		googleKeys = &googleJWKSCache{}
	}()

	token := testGoogleToken(t, key, "e2e-kid", map[string]any{
		"iss": "https://accounts.google.com", "aud": "client", "sub": "s",
		"email": "e2e@example.com", "email_verified": true, "exp": time.Now().Add(time.Hour).Unix(),
	})

	identity, err := VerifyGoogleIDToken(token, "client")
	if err != nil {
		t.Fatalf("expected valid token via fake JWKS server, got: %v", err)
	}
	if identity.Email != "e2e@example.com" {
		t.Errorf("expected e2e@example.com, got %q", identity.Email)
	}
}
