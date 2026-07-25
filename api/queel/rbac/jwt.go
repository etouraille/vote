package rbac

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims is what queel's JWTs carry: a subject identifying the caller (the
// *calling application's* user ID — queel doesn't require it to be an rbac
// UUID, just whatever the issuer wants echoed back), plus the permission
// mask looked up from this package's Store at issuance time. A holder can
// then check User.Can-equivalent authorization locally (see Allows) without
// a round trip to the rbac socket on every request.
type Claims struct {
	Subject   string  `json:"sub"`
	Root      bool    `json:"root,omitempty"`
	Perms     PermBit `json:"perms"`
	ExpiresAt int64   `json:"exp"`
}

var (
	// ErrExpiredToken is returned by VerifyToken for a well-formed, correctly
	// signed token whose ExpiresAt has passed.
	ErrExpiredToken = errors.New("rbac: token expired")

	// ErrInvalidToken is returned by VerifyToken for anything malformed or
	// whose signature doesn't match — deliberately not distinguished further,
	// so callers can't use error details to probe the signing secret.
	ErrInvalidToken = errors.New("rbac: invalid token")
)

// Allows reports whether claims permit action, mirroring User.Can: a root
// claim bypasses the mask entirely.
func (c Claims) Allows(action Action) bool {
	if c.Root {
		return true
	}
	return c.Perms.Allows(action)
}

// jwtHeader is the sole header this package ever produces: HS256 is enough
// for a symmetric secret shared between the token issuer and whoever
// verifies it (typically the same deployment, sometimes queeld too) — there
// is no need for algorithm negotiation.
const jwtHeader = `{"alg":"HS256","typ":"JWT"}`

// SignToken encodes claims as a compact HS256 JWT signed with secret. The
// result is a standard three-part JWT (any RFC 7519 library can decode it),
// even though this package only ever needs to produce and consume it itself.
func SignToken(claims Claims, secret []byte) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString([]byte(jwtHeader)) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	return signingInput + "." + sign(signingInput, secret), nil
}

// VerifyToken checks token's HS256 signature against secret and its
// expiry, returning the claims it carries if both hold.
func VerifyToken(token string, secret []byte) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}

	signingInput := parts[0] + "." + parts[1]
	expected := sign(signingInput, secret)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return Claims{}, ErrInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return Claims{}, ErrExpiredToken
	}
	return claims, nil
}

func sign(signingInput string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
