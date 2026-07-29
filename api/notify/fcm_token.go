package notify

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// fcmScope is the only OAuth scope this needs: sending messages, not
// managing the project.
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// serviceAccount is the subset of a Google service-account JSON key file
// needed to mint an access token. The file is what "Firebase console >
// Project settings > Service accounts > Generate new private key" hands
// over; the fields it also contains (project_number, client_id, ...) are
// not used here.
type serviceAccount struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI    string `json:"token_uri"`
}

func parseServiceAccount(raw []byte) (*serviceAccount, *rsa.PrivateKey, error) {
	var account serviceAccount
	if err := json.Unmarshal(raw, &account); err != nil {
		return nil, nil, fmt.Errorf("parsing service account json: %w", err)
	}
	if account.ClientEmail == "" || account.PrivateKey == "" || account.ProjectID == "" {
		return nil, nil, errors.New("service account json is missing client_email, private_key or project_id")
	}
	if account.TokenURI == "" {
		account.TokenURI = "https://oauth2.googleapis.com/token"
	}

	key, err := parsePrivateKey(account.PrivateKey)
	if err != nil {
		return nil, nil, err
	}
	return &account, key, nil
}

func parsePrivateKey(pemKey string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("service account private_key is not valid PEM")
	}

	// Google issues PKCS#8; PKCS#1 is accepted too so a hand-converted key
	// doesn't fail for a reason that has nothing to do with the caller.
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("service account private_key is not an RSA key")
		}
		return rsaKey, nil
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing service account private_key: %w", err)
	}
	return key, nil
}

// tokenSource mints and caches Google OAuth access tokens from a service
// account, the way golang.org/x/oauth2/google would — done by hand here to
// keep this module's dependency list as short as it is.
type tokenSource struct {
	account *serviceAccount
	key     *rsa.PrivateKey
	client  *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// tokenExpiryMargin renews slightly early, so a token that passes the
// check here can't expire in flight on the request that follows.
const tokenExpiryMargin = 30 * time.Second

func (s *tokenSource) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && time.Now().Before(s.expiresAt.Add(-tokenExpiryMargin)) {
		return s.token, nil
	}

	assertion, err := s.signedAssertion()
	if err != nil {
		return "", err
	}

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.account.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting access token: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decoding access token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || body.AccessToken == "" {
		return "", fmt.Errorf("access token refused (%d): %s %s", resp.StatusCode, body.Error, body.Description)
	}

	s.token = body.AccessToken
	s.expiresAt = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	return s.token, nil
}

// signedAssertion builds the RS256 JWT that is exchanged for an access
// token — the "JWT bearer" flow described at
// https://developers.google.com/identity/protocols/oauth2/service-account.
func (s *tokenSource) signedAssertion() (string, error) {
	now := time.Now()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":   s.account.ClientEmail,
		"scope": fcmScope,
		"aud":   s.account.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("signing assertion: %w", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
