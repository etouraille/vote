package rbac

// Verifying a Google Sign-In ID token: fetch Google's own published RS256
// public keys (JWKS) and check the token's signature against them by
// hand — same reasoning as jwt.go's SignToken/VerifyToken being hand-rolled
// HS256 rather than pulling in a third-party JWT library: this package
// needs exactly one thing from a JWT verifier, so a whole dependency (and
// its transitive tree) isn't worth it for that.
//
// This is unrelated to SignToken/VerifyToken, which sign/verify OUR OWN
// session tokens (HS256, a symmetric secret only api and this package
// know). A Google ID token is signed by GOOGLE (RS256, asymmetric, checked
// against Google's freely published public keys) — a caller uses
// VerifyGoogleIDToken once, at the moment someone signs in, purely to
// establish "this really is this Google account, Google says so"; anything
// after that (issuing a session) is the exact same SignToken/Claims flow
// password login already uses.

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// googleJWKSURL is Google's own published RS256 public keys for verifying
// ID tokens it issues — see
// https://developers.google.com/identity/openid-connect/openid-connect#discovery.
// Rotated by Google periodically, hence the cache below rather than a
// per-login fetch.
var googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

// googleIssuers are the two values Google's ID tokens use interchangeably
// as `iss` — see
// https://developers.google.com/identity/openid-connect/openid-connect#validatinganidtoken.
var googleIssuers = map[string]bool{
	"https://accounts.google.com": true,
	"accounts.google.com":         true,
}

// GoogleIdentity is what VerifyGoogleIDToken confirms about the signed-in
// Google account — enough for a caller to look up or create a local user
// by email, nothing more.
type GoogleIdentity struct {
	// Subject is Google's own stable per-account ID (the "sub" claim).
	Subject       string
	Email         string
	EmailVerified bool
}

var (
	ErrGoogleTokenInvalid = errors.New("rbac: invalid google id token")
	ErrGoogleTokenExpired = errors.New("rbac: expired google id token")
)

// VerifyGoogleIDToken verifies idToken was really issued by Google for
// clientID — signature, issuer, audience, and expiry are all checked, none
// of it taken on the client's word — and returns the account it
// identifies. It says nothing about whether that account already has a
// local user; the caller decides look-up-vs-create from the result.
func VerifyGoogleIDToken(idToken string, clientID string) (GoogleIdentity, error) {
	return verifyGoogleIDToken(idToken, clientID, googleKeys.get)
}

func verifyGoogleIDToken(idToken string, clientID string, lookupKey func(kid string) (*rsa.PublicKey, error)) (GoogleIdentity, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}
	if header.Alg != "RS256" {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}

	key, err := lookupKey(header.Kid)
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("rbac: %w", err)
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}
	var claims struct {
		Iss           string `json:"iss"`
		Aud           string `json:"aud"`
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"` // bool or "true"/"false", depending on issuer
		Exp           int64  `json:"exp"`
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}

	if !googleIssuers[claims.Iss] {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}
	if clientID == "" || claims.Aud != clientID {
		return GoogleIdentity{}, ErrGoogleTokenInvalid
	}
	if time.Now().Unix() >= claims.Exp {
		return GoogleIdentity{}, ErrGoogleTokenExpired
	}

	emailVerified := false
	switch v := claims.EmailVerified.(type) {
	case bool:
		emailVerified = v
	case string:
		emailVerified = v == "true"
	}

	return GoogleIdentity{
		Subject:       claims.Sub,
		Email:         strings.ToLower(claims.Email),
		EmailVerified: emailVerified,
	}, nil
}

// googleKeys caches Google's JWKS for googleKeysTTL so a login doesn't cost
// a round trip to Google on top of verifying the signature itself; a stale
// cache is served on a fetch error rather than failing every login just
// because Google's certs endpoint had one bad moment.
var googleKeys = &googleJWKSCache{}

const googleKeysTTL = 1 * time.Hour

type googleJWKSCache struct {
	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

func (c *googleJWKSCache) get(kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.keys == nil || time.Since(c.fetched) > googleKeysTTL {
		keys, err := fetchGoogleJWKS()
		if err != nil {
			if key, ok := c.keys[kid]; ok {
				return key, nil
			}
			return nil, err
		}
		c.keys = keys
		c.fetched = time.Now()
	}

	key, ok := c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("no google signing key for kid %q", kid)
	}
	return key, nil
}

type googleJWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func fetchGoogleJWKS() (map[string]*rsa.PublicKey, error) {
	resp, err := http.Get(googleJWKSURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching google jwks: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []googleJWK `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, err
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

func rsaPublicKeyFromJWK(k googleJWK) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}

	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}

	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}
