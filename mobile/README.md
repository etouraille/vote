# mobile

A new Flutter project.

## Getting Started

This project is a starting point for a Flutter application.

A few resources to get you started if this is your first Flutter project:

- [Learn Flutter](https://docs.flutter.dev/get-started/learn-flutter)
- [Write your first Flutter app](https://docs.flutter.dev/get-started/codelab)
- [Flutter learning resources](https://docs.flutter.dev/reference/learning-resources)

For help getting started with Flutter development, view the
[online documentation](https://docs.flutter.dev/), which offers tutorials,
samples, guidance on mobile development, and a full API reference.

## notifications
pour permettre les notification android ... prévoir un sytème de notification multicanal dans le back
Ambitieux — je commence par l'état des lieux avant de concevoir.

Bash Check API dependencies and subscriber lookup
IN
cd /home/etouraille/src/vote/api && cat go.mod && echo "=== abonnés d'un texte ? ===" && grep -rn "func (r \*Repository).*Subscri\|SubscribersFor" queel/repository.go

OUT
module vote-api

go 1.25.0

require (
	github.com/etouraille/queel v0.0.0-00010101000000-000000000000
	github.com/lib/pq v1.12.3
	golang.org/x/crypto v0.54.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.47.0 // indirect
	modernc.org/libc v1.74.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.54.0 // indirect
)

replace github.com/etouraille/queel => ./queel
=== abonnés d'un texte ? ===
queel/repository.go:392:func (r *Repository) Subscribe(userID, textID string) (*Subscription, error) {
queel/repository.go:412:func (r *Repository) IsSubscribed(userID, textID string) (bool, error) {
queel/repository.go:421:func (r *Repository) SubscriptionsForUser(userID string) ([]string, error) {
queel/repository.go:439:func (r *Repository) DeleteUserSubscriptions(userID string) error {
Bash List API files and mail sending helpers
IN
ls *.go && echo "=== mail ===" && grep -rn "func.*[Ss]end" mail.go 2>/dev/null | head

OUT
admin.go
auth.go
auth_google_test.go
cors.go
cors_test.go
embed.go
health.go
http.go
mail.go
main.go
middleware.go
qdrant.go
scheduled_close.go
scheduled_close_test.go
search.go
slots.go
store.go
subscriptions.go
texts.go
votes.go
=== mail ===
mail.go:25:func sendValidationEmail(toEmail string, code int) error {
Bash Read subscription storage in queel
IN
cd /home/etouraille/src/vote/api/queel && sed -n '380,450p' repository.go

OUT
func (r *Repository) IsSuperseded(textID string) (bool, error) {
	_, found, err := r.store.Get(supersededByKey(textID))
	if err != nil {
		return false, err
	}
	return found, nil
}

// Subscribe records that userID wants to follow textID — see IsSubscribed
// and SubscriptionsForUser. Idempotent: subscribing again just refreshes
// CreatedAt. Fails with ErrNotFound rather than silently subscribing to a
// text that doesn't exist.
func (r *Repository) Subscribe(userID, textID string) (*Subscription, error) {
	if _, err := r.Text(textID); err != nil {
		return nil, err
	}

	sub := &Subscription{UserID: userID, TextID: textID, CreatedAt: time.Now()}
	payload, err := json.Marshal(sub)
	if err != nil {
		return nil, err
	}
	if err := r.store.WriteBatch([]WriteOp{
		{Key: subscriptionKey(textID, userID), Value: payload},
		{Key: subscriptionIndexKey(userID, textID), Value: []byte(textID)},
	}); err != nil {
		return nil, err
	}
	return sub, nil
}

// IsSubscribed reports whether userID currently follows textID.
func (r *Repository) IsSubscribed(userID, textID string) (bool, error) {
	_, found, err := r.store.Get(subscriptionKey(textID, userID))
	if err != nil {
		return false, err
	}
	return found, nil
}

// SubscriptionsForUser lists the ID of every text userID currently follows.
func (r *Repository) SubscriptionsForUser(userID string) ([]string, error) {
	kvs, err := r.store.Scan(subscriptionIndexPrefix(userID))
	if err != nil {
		return nil, err
	}
	textIDs := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		textIDs = append(textIDs, string(kv.Value))
	}
	return textIDs, nil
}

// DeleteUserSubscriptions removes every subscription userID ever made —
// both the direct record (subscriptionKey) and its entry in the per-user
// index (subscriptionIndexKey) that listed it. Scanning the index directly
// gives us the exact textID for each one, so unlike DeleteUserVotes this
// doesn't need a full scan of every subscription ever made plus a suffix
// match.
func (r *Repository) DeleteUserSubscriptions(userID string) error {
	kvs, err := r.store.Scan(subscriptionIndexPrefix(userID))
	if err != nil {
		return err
	}

	var ops []WriteOp
	for _, kv := range kvs {
		textID := string(kv.Value)
		ops = append(ops,
			WriteOp{Key: kv.Key, Tombstone: true},
			WriteOp{Key: subscriptionKey(textID, userID), Tombstone: true},
Bash Find subscription key helpers
IN
grep -rn "func subscriptionKey\|func subscriptionIndexKey\|func subscriptionIndexPrefix\|func subscriptionPrefix" *.go

OUT
repository.go:127:func subscriptionKey(textID, userID string) []byte {
repository.go:131:func subscriptionIndexKey(userID, textID string) []byte {
repository.go:135:func subscriptionIndexPrefix(userID string) []byte {
Bash Read subscription key helpers
IN
sed -n '120,140p' repository.go

OUT
// subscriptionKey is the primary record: does userID follow textID. Stored
// alongside subscriptionIndexKey below under a different key namespace, the
// same double-storage fragment/fragmentindex already uses — one for the
// direct "is this user subscribed to this text" check, one for "list every
// text this user is subscribed to" as a cheap prefix Scan instead of a full
// scan of every subscription ever made.
func subscriptionKey(textID, userID string) []byte {
	return []byte(fmt.Sprintf("subscription/%s/%s", textID, userID))
}

func subscriptionIndexKey(userID, textID string) []byte {
	return []byte(fmt.Sprintf("subscriptionindex/%s/%s", userID, textID))
}

func subscriptionIndexPrefix(userID string) []byte {
	return []byte(fmt.Sprintf("subscriptionindex/%s/", userID))
}

func voteKey(fragmentID, userID string) []byte {
	return []byte(fmt.Sprintf("vote/%s/%s", fragmentID, userID))
C'est un gros morceau — je le découpe et je le suis.

Update Todos

queel : SubscribersForText + test

api : coeur multicanal (Channel, dispatcher, canaux log/email/push)

api : enregistrement des jetons d'appareil + route

api : déclenchement à la modification d'un texte, vers les abonnés

api : documentation .env.example

mobile : Firebase via .env + enregistrement du jeton + procédure Android documentée

Vérification : build, vet, tests, analyze
Edit repository.go
Added 8 lines
Edit repository.go
Added 24 lines
Bash Find existing subscription tests
IN
grep -n "func TestSubscriptionsForUser\|func TestSubscribe" repository_test.go | head -4

OUT
846:func TestSubscribeUnknownText(t *testing.T) {
853:func TestSubscriptionsForUserListsSubscribedTexts(t *testing.T) {
Bash Read the existing subscriptions test for style
IN
sed -n '853,885p' repository_test.go

OUT
func TestSubscriptionsForUserListsSubscribedTexts(t *testing.T) {
	repo := newTestRepository(t)

	var ids []string
	for i := 0; i < 3; i++ {
		text, err := repo.CreateText(fmt.Sprintf("Text %d", i), "content", "creator")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, text.ID)
	}

	if _, err := repo.Subscribe("alice", ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Subscribe("alice", ids[2]); err != nil {
		t.Fatal(err)
	}
	// ids[1] is never subscribed to, and bob's own subscription must not
	// leak into alice's list.
	if _, err := repo.Subscribe("bob", ids[1]); err != nil {
		t.Fatal(err)
	}

	subscribed, err := repo.SubscriptionsForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range subscribed {
		got[id] = true
	}
	if len(got) != 2 || !got[ids[0]] || !got[ids[2]] {
Bash Add and run SubscribersForText tests
IN
cat >> repository_test.go <<'EOF'

func TestSubscribersForTextListsFollowers(t *testing.T) {
	repo := newTestRepository(t)

	watched, err := repo.CreateText("Suivi", "content", "creator")
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.CreateText("Autre", "content", "creator")
	if err != nil {
		t.Fatal(err)
	}

	for _, user := range []string{"alice", "bob"} {
		if _, err := repo.Subscribe(user, watched.ID); err != nil {
			t.Fatal(err)
		}
	}
	// carol follows a different text: her subscription must not leak into
	// the followers of the one being asked about.
	if _, err := repo.Subscribe("carol", other.ID); err != nil {
		t.Fatal(err)
	}

	subscribers, err := repo.SubscribersForText(watched.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range subscribers {
		got[id] = true
	}
	if len(got) != 2 || !got["alice"] || !got["bob"] {
		t.Fatalf("expected alice and bob, got %v", subscribers)
	}
}

// A text nobody follows must come back empty rather than erroring — the
// ordinary case for a text that was just created.
func TestSubscribersForTextWithoutSubscribersIsEmpty(t *testing.T) {
	repo := newTestRepository(t)

	text, err := repo.CreateText("Personne", "content", "creator")
	if err != nil {
		t.Fatal(err)
	}

	subscribers, err := repo.SubscribersForText(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subscribers) != 0 {
		t.Fatalf("expected no subscribers, got %v", subscribers)
	}
}
EOF
export PATH=/home/etouraille/go/bin:$PATH
go test ./ -run SubscribersForText -v 2>&1 | grep -v "^warning:" | tail -8

OUT
    repository_test.go:1678: expected alice and bob, got [alice bob creator]
--- FAIL: TestSubscribersForTextListsFollowers (0.00s)
=== RUN   TestSubscribersForTextWithoutSubscribersIsEmpty
    repository_test.go:1697: expected no subscribers, got [creator]
--- FAIL: TestSubscribersForTextWithoutSubscribersIsEmpty (0.00s)
FAIL
FAIL	github.com/etouraille/queel	0.003s
FAIL
Le test révèle un comportement que j'ignorais : le créateur est abonné d'office à son texte. Vérifions.

Bash Confirm the creator is auto-subscribed
IN
grep -n "func (r \*Repository) CreateText" -A 30 repository.go | grep -nE "Subscribe|subscription|WriteOp|return" | head -8

OUT
4:171-		return nil, err
10:177-		return nil, err
16:183-		return nil, err
19:186-	if err := r.store.WriteBatch([]WriteOp{
21:188-		{Key: subscriptionKey(id, authorID), Value: subPayload},
22:189-		{Key: subscriptionIndexKey(authorID, id), Value: []byte(id)},
24:191-		return nil, err
26:193-	return text, nil
Confirmé : repository.go:188 — CreateText écrit d'office l'abonnement de l'auteur. Mes tests avaient tort, pas le code. Je les corrige pour décrire le comportement réel.

Edit repository_test.go
Added 3 lines
Bash Re-run the SubscribersForText tests
IN
export PATH=/home/etouraille/go/bin:$PATH
go test ./ -run SubscribersForText 2>&1 | grep -v "^warning:" | tail -4

OUT
ok  	github.com/etouraille/queel	0.003s
Update Todos

queel : SubscribersForText + test

api : coeur multicanal (Channel, dispatcher, canaux log/email/push)

api : enregistrement des jetons d'appareil + route

api : déclenchement à la modification d'un texte, vers les abonnés

api : documentation .env.example

mobile : Firebase via .env + enregistrement du jeton + procédure Android documentée

Vérification : build, vet, tests, analyze
Maintenant le cœur multicanal côté API.

Write notify.go
113 lines
// Package notify fans one event out to several delivery channels.
//
// The point of the indirection is that "who should hear about this" and
// "how does it reach them" are separate concerns that change at different
// rates: call sites name an audience and an event, and adding email or a
// webhook alongside push later touches only this package.
//
// A Channel receives the whole recipient list at once rather than being
// called per recipient, because most transports batch far more cheaply
// than they send one by one — FCM takes up to 500 tokens per request.
package notify

import (
	"context"
	"log"
	"sync"
)

// Recipient is one person to reach, with everything the channels might
// need to do so. A channel simply skips anyone it has no address for: a
// user with no registered device is not an error for the push channel,
// just someone it cannot reach.
type Recipient struct {
	UserID string
	Email  string
	// DeviceTokens is every device this user has registered — someone
	// signed in on a phone and a tablet gets both.
	DeviceTokens []string
}

// Notification is what happened, in terms a channel can render. Title and
// Body are for humans; Data is the machine-readable payload a mobile app
// uses to deep-link, and is ignored by channels that have no equivalent.
type Notification struct {
	Title string
	Body  string
	Data  map[string]string
}

// Channel is one way of delivering a notification. Implementations must be
// safe for concurrent use: Dispatcher calls them in parallel.
type Channel interface {
	// Name identifies the channel in logs.
	Name() string

	// Send delivers to everyone it can reach. Returning an error means the
	// channel as a whole failed; being unable to reach one recipient is
	// not an error, it is the normal case for anyone this channel has no
	// address for.
	Send(ctx context.Context, n Notification, recipients []Recipient) error
}

// Dispatcher sends one notification through every configured channel.
//
// Channels are independent: one failing must not stop the others, so a
// failure is logged and the rest proceed. Notifying is a side effect of
// whatever the user actually asked for — nobody's text edit should fail
// because a push provider was down.
type Dispatcher struct {
	channels []Channel
}

// NewDispatcher builds a dispatcher over the channels that are actually
// configured. Passing none is valid and makes every Notify call a no-op —
// the shape a deployment that hasn't set up any provider takes.
func NewDispatcher(channels ...Channel) *Dispatcher {
	enabled := make([]Channel, 0, len(channels))
	for _, channel := range channels {
		if channel != nil {
			enabled = append(enabled, channel)
		}
	}
	return &Dispatcher{channels: enabled}
}

// Channels lists the configured channel names, for logging at startup what
// a deployment will actually be able to deliver.
func (d *Dispatcher) Channels() []string {
	names := make([]string, 0, len(d.channels))
	for _, channel := range d.channels {
		names = append(names, channel.Name())
	}
	return names
}

// Notify delivers n to recipients over every channel, in parallel, and
// waits for them all.
//
// It never returns an error: there is no useful way for a caller to react
// to "the email channel was down", and propagating it would tempt call
// sites into failing the user's actual request over it. Failures are
// logged instead.
//
// An empty recipient list short-circuits — the common case once the actor
// has been excluded from their own text's followers.
func (d *Dispatcher) Notify(ctx context.Context, n Notification, recipients []Recipient) {
	if len(d.channels) == 0 || len(recipients) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, channel := range d.channels {
		wg.Add(1)
		go func(channel Channel) {
			defer wg.Done()
			if err := channel.Send(ctx, n, recipients); err != nil {
				log.Printf("notify: channel %s failed: %v", channel.Name(), err)
			}
		}(channel)
	}
	wg.Wait()
}
Edit notify.go
Modified
Write log_channel.go
27 lines
package notify

import (
	"context"
	"log"
)

// LogChannel writes notifications to the process log instead of delivering
// them anywhere.
//
// It exists so a deployment with no provider configured still shows that
// the fan-out fired, and to whom — the difference between "notifications
// aren't set up" and "notifications are broken" is otherwise invisible,
// and that ambiguity costs far more to debug than this costs to keep.
type LogChannel struct{}

func (LogChannel) Name() string { return "log" }

func (LogChannel) Send(_ context.Context, n Notification, recipients []Recipient) error {
	userIDs := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		userIDs = append(userIDs, recipient.UserID)
	}
	log.Printf("notify(log): %q — %q → %v", n.Title, n.Body, userIDs)
	return nil
}
Maintenant le canal push FCM. L'API v1 exige un jeton OAuth2 signé par un compte de service — je l'implémente sans dépendance supplémentaire.

Write fcm_token.go
177 lines
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
Write push_channel.go
162 lines
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// maxConcurrentPushes bounds how many FCM requests are in flight at once.
// The v1 API sends to one token per request, so a text with many followers
// would otherwise open one connection per device all at once.
const maxConcurrentPushes = 8

// PushChannel delivers over Firebase Cloud Messaging, HTTP v1.
//
// v1 has no multicast: one request per device token. Hence the bounded
// parallelism below rather than a single batched call.
type PushChannel struct {
	projectID string
	tokens    *tokenSource
	client    *http.Client

	// invalidToken is called with a device token FCM has rejected as
	// unregistered, so the caller can forget it. Uninstalling an app leaves
	// its token behind forever otherwise, and every later notification pays
	// for a request that cannot succeed.
	invalidToken func(token string)
}

// NewPushChannel reads a Google service-account key file and prepares an
// FCM sender. Returns nil (and no error) when path is empty: push is
// optional, and a deployment that hasn't configured it should start
// normally without it, not fail.
func NewPushChannel(path string, invalidToken func(token string)) (*PushChannel, error) {
	if path == "" {
		return nil, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading service account file: %w", err)
	}
	account, key, err := parseServiceAccount(raw)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	return &PushChannel{
		projectID:    account.ProjectID,
		tokens:       &tokenSource{account: account, key: key, client: client},
		client:       client,
		invalidToken: invalidToken,
	}, nil
}

func (*PushChannel) Name() string { return "push" }

func (p *PushChannel) Send(ctx context.Context, n Notification, recipients []Recipient) error {
	deviceTokens := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		deviceTokens = append(deviceTokens, recipient.DeviceTokens...)
	}
	if len(deviceTokens) == 0 {
		// Nobody in this audience has a registered device. Not a failure:
		// the push channel simply has no address for them.
		return nil
	}

	// One access token for the whole fan-out, fetched before the workers
	// start so they don't all race to mint it.
	accessToken, err := p.tokens.accessToken(ctx)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	slots := make(chan struct{}, maxConcurrentPushes)
	for _, deviceToken := range deviceTokens {
		wg.Add(1)
		slots <- struct{}{}
		go func(deviceToken string) {
			defer wg.Done()
			defer func() { <-slots }()

			// Per-device failures are logged, never returned: one dead
			// token must not deprive every other follower of the message.
			if err := p.sendOne(ctx, accessToken, deviceToken, n); err != nil {
				log.Printf("notify(push): device token %s…: %v", truncateToken(deviceToken), err)
			}
		}(deviceToken)
	}
	wg.Wait()
	return nil
}

func (p *PushChannel) sendOne(ctx context.Context, accessToken, deviceToken string, n Notification) error {
	payload := map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"notification": map[string]string{
				"title": n.Title,
				"body":  n.Body,
			},
			// Mirrored into data so the app can act on the event (deep-link
			// to the text) whether or not the OS surfaced the notification.
			"data": n.Data,
			"android": map[string]any{
				"priority": "high",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", p.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	// 404 UNREGISTERED and 400 INVALID_ARGUMENT on the token are FCM's way
	// of saying this device is gone for good — worth forgetting, unlike a
	// 5xx which is worth retrying next time.
	if resp.StatusCode == http.StatusNotFound && p.invalidToken != nil {
		p.invalidToken(deviceToken)
	}
	return fmt.Errorf("fcm returned %d: %s", resp.StatusCode, responseBody)
}

// truncateToken keeps logs readable and avoids writing whole device tokens
// to disk — they are credentials for pushing to that device.
func truncateToken(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:12]
}
Read mail.go
Edit mail.go
Added 44 lines
Read mail.go (lines 71-120)
Edit mail.go
Removed 29 lines
Write email_channel.go
64 lines
package notify

import (
	"context"
	"log"
	"sync"
)

// maxConcurrentEmails bounds in-flight sends, for the same reason the push
// channel bounds its own: one transactional-email request per recipient,
// and a popular text should not open one connection per follower at once.
const maxConcurrentEmails = 4

// EmailChannel delivers notifications as plain-text email.
//
// The actual sending is injected rather than imported, so this package
// stays free of any particular provider — the api wires it to its existing
// Brevo client, and a test or another deployment can pass anything with
// the same shape.
type EmailChannel struct {
	Send_ func(to, subject, body string) error
}

// NewEmailChannel returns nil when send is nil, so an unconfigured
// deployment simply has no email channel rather than one that fails on
// every notification.
func NewEmailChannel(send func(to, subject, body string) error) *EmailChannel {
	if send == nil {
		return nil
	}
	return &EmailChannel{Send_: send}
}

func (*EmailChannel) Name() string { return "email" }

func (e *EmailChannel) Send(_ context.Context, n Notification, recipients []Recipient) error {
	var wg sync.WaitGroup
	slots := make(chan struct{}, maxConcurrentEmails)

	for _, recipient := range recipients {
		if recipient.Email == "" {
			// No address for this person — not a failure, just someone
			// this channel cannot reach.
			continue
		}

		wg.Add(1)
		slots <- struct{}{}
		go func(recipient Recipient) {
			defer wg.Done()
			defer func() { <-slots }()

			// Logged, not returned: one bounced address must not stop the
			// rest of the fan-out.
			if err := e.Send_(recipient.Email, n.Title, n.Body); err != nil {
				log.Printf("notify(email): %s: %v", recipient.Email, err)
			}
		}(recipient)
	}

	wg.Wait()
	return nil
}
Send_ est un nom disgracieux né d'une collision avec la méthode. Je le rends privé.

Bash Rename the injected sender field
IN
cd /home/etouraille/src/vote/api/notify && sed -i 's/\tSend_ func(to, subject, body string) error/\tsend func(to, subject, body string) error/; s/return \&EmailChannel{Send_: send}/return \&EmailChannel{send: send}/; s/e\.Send_(/e.send(/' email_channel.go && grep -n "send\b\|Send_" email_channel.go | head -6

OUT
21:	send func(to, subject, body string) error
24:// NewEmailChannel returns nil when send is nil, so an unconfigured
27:func NewEmailChannel(send func(to, subject, body string) error) *EmailChannel {
28:	if send == nil {
31:	return &EmailChannel{send: send}
55:			if err := e.send(recipient.Email, n.Title, n.Body); err != nil {
Le canal email est propre. Passons au stockage des jetons d'appareil.

Update Todos

queel : SubscribersForText + test

api : coeur multicanal (Channel, dispatcher, canaux log/email/push)

api : enregistrement des jetons d'appareil + route

api : déclenchement à la modification d'un texte, vers les abonnés

api : documentation .env.example

mobile : Firebase via .env + enregistrement du jeton + procédure Android documentée

Vérification : build, vet, tests, analyze
Bash Inspect the Postgres store schema and helpers
IN
cd /home/etouraille/src/vote/api && grep -n "CREATE TABLE\|func.*[Mm]igrate\|ExecContext" store.go | head -12 && echo "=== UserByID / UsersByIDs ? ===" && grep -n "func (s \*Store)" store.go | head -20

OUT
50:	_, err := s.db.ExecContext(ctx,
73:	_, err := s.db.ExecContext(ctx,
88:	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
167:	result, err := s.db.ExecContext(ctx, `UPDATE users SET rbac_uuid = $1 WHERE id = $2`, rbacUUID, id)
182:	result, err := s.db.ExecContext(ctx,
=== UserByID / UsersByIDs ? ===
43:func (s *Store) CreateUser(ctx context.Context, email, passwordHash string, validationCode int, pseudo string) (*User, error) {
71:func (s *Store) CreateUserFromGoogle(ctx context.Context, email, passwordHash, pseudo string) (*User, error) {
87:func (s *Store) DeleteUser(ctx context.Context, id string) error {
92:func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
100:func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
108:func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
156:func (s *Store) scanUser(row *sql.Row) (*User, error) {
166:func (s *Store) SetRbacUUID(ctx context.Context, id, rbacUUID string) error {
181:func (s *Store) ConfirmUser(ctx context.Context, email string, code int) error {
Bash Read the store setup and find schema creation
IN
sed -n '1,45p' store.go && echo "=== schéma / migrations ===" && grep -rn "CREATE TABLE" --include="*.go" --include="*.sql" . | grep -v queel | head

OUT
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/lib/pq"
)

type User struct {
	ID             string
	Email          string
	PasswordHash   string
	ValidationCode *int
	// RbacUUID is this user's ID in queel's rbac directory (see
	// queel/rbac), nil until an admin assigns permissions to them.
	RbacUUID *string
	// Pseudo is an optional display name set at registration; nil if the
	// user never set one, in which case the front falls back to email.
	Pseudo *string
}

var (
	ErrEmailTaken            = errors.New("email already registered")
	ErrUserNotFound          = errors.New("user not found")
	ErrInvalidValidationCode = errors.New("invalid validation code")
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateUser registers a new account. pseudo may be empty — stored as NULL,
// not an empty string, so callers can tell "no pseudo set" apart from a
// (rejected before it gets here) blank one.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string, validationCode int, pseudo string) (*User, error) {
	id := newRandomHex(8)
	var pseudoPtr *string
=== schéma / migrations ===
migrations/001_create_users.sql:1:CREATE TABLE IF NOT EXISTS users (
Bash Inspect migrations and how they are applied
IN
ls migrations/ && cat migrations/001_create_users.sql && echo "=== comment les migrations sont appliquées ===" && grep -rn "migrations" --include="*.go" . | grep -v queel | head -5

OUT
001_create_users.sql
002_add_validation_code.sql
003_add_rbac_uuid.sql
004_add_pseudo.sql
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
=== comment les migrations sont appliquées ===
Bash Check how migrations are embedded and run
IN
grep -rn "embed\|\.sql" embed.go main.go | head -10 && echo "=== embed.go ===" && cat embed.go

OUT
embed.go:11:// embedder turns text into a vector for similarity search. The question is
embed.go:12:// embedded the same way as the finalized texts it's compared against, so
embed.go:14:type embedder interface {
embed.go:36:	Embedding []float32 `json:"embedding"`
embed.go:45:	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embeddings", bytes.NewReader(body))
embed.go:58:		return nil, fmt.Errorf("ollama embeddings: unexpected status %d", resp.StatusCode)
embed.go:66:		return nil, fmt.Errorf("ollama embeddings: empty embedding returned")
main.go:148:		ollamaModel = "nomic-embed-text"
main.go:150:	embed := newOllamaEmbedder(ollamaBaseURL, ollamaModel)
main.go:162:	searchIndex := newSearchIndexer(embed, qdrant, pruneSuperseded)
=== embed.go ===
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// embedder turns text into a vector for similarity search. The question is
// embedded the same way as the finalized texts it's compared against, so
// both sides land in the same vector space.
type embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// ollamaEmbedder calls a self-hosted Ollama instance — no external API key,
// matching the rest of this stack (queel, Postgres, Qdrant) being self-hosted.
type ollamaEmbedder struct {
	baseURL string
	model   string
	http    *http.Client
}

func newOllamaEmbedder(baseURL, model string) *ollamaEmbedder {
	return &ollamaEmbedder{baseURL: baseURL, model: model, http: http.DefaultClient}
}

type ollamaEmbeddingsRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbeddingsResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (o *ollamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(ollamaEmbeddingsRequest{Model: o.model, Prompt: text})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embeddings: unexpected status %d", resp.StatusCode)
	}

	var parsed ollamaEmbeddingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Embedding) == 0 {
		return nil, fmt.Errorf("ollama embeddings: empty embedding returned")
	}
	return parsed.Embedding, nil
}
Bash Check whether migrations are mounted into Postgres
IN
sed -n '1,16p' docker-compose.yml

OUT
services:
  postgres:
    image: postgres:16-alpine
    container_name: vote-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: vote
      POSTGRES_PASSWORD: vote
      POSTGRES_DB: vote
    ports:
      - "5432:5432"
    volumes:
      - vote-postgres-data:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d:ro

  qdrant:
Write 005_create_device_tokens.sql
18 lines
-- Push notification targets: one row per device a user has signed in on
-- (see notify.PushChannel).
--
-- The token, not (user, device), is the primary key: a token identifies an
-- app installation, and the same installation can end up signed in as a
-- different user. Registering an existing token therefore reassigns it
-- rather than duplicating it, so the previous owner stops receiving
-- notifications on a device that is no longer theirs.
CREATE TABLE IF NOT EXISTS device_tokens (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS device_tokens_user_id_idx ON device_tokens (user_id);
Write devices.go
143 lines
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/lib/pq"
)

// maxDeviceTokenRunes bounds what a client can store. FCM registration
// tokens run to a few hundred characters; this leaves ample room while
// keeping a broken client from writing arbitrarily large rows.
const maxDeviceTokenRunes = 4096

// RegisterDeviceToken records that userID can be reached at token.
//
// Upsert on the token, not on (user, token): a token identifies an app
// installation, so re-registering one that already exists moves it to its
// new owner rather than leaving the previous user subscribed to a device
// that is no longer theirs.
func (s *Store) RegisterDeviceToken(ctx context.Context, userID, token, platform string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO device_tokens (token, user_id, platform)
		VALUES ($1, $2, $3)
		ON CONFLICT (token) DO UPDATE
			SET user_id = EXCLUDED.user_id,
			    platform = EXCLUDED.platform,
			    updated_at = now()`,
		token, userID, platform)
	return err
}

// DeviceTokensForUsers maps each user id to the tokens of every device they
// have registered. Users with no device are simply absent from the map.
func (s *Store) DeviceTokensForUsers(ctx context.Context, userIDs []string) (map[string][]string, error) {
	tokens := map[string][]string{}
	if len(userIDs) == 0 {
		return tokens, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, token FROM device_tokens WHERE user_id = ANY($1)`, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var userID, token string
		if err := rows.Scan(&userID, &token); err != nil {
			return nil, err
		}
		tokens[userID] = append(tokens[userID], token)
	}
	return tokens, rows.Err()
}

// EmailsForUsers maps each user id to their email address, for channels
// that reach people rather than devices.
func (s *Store) EmailsForUsers(ctx context.Context, userIDs []string) (map[string]string, error) {
	emails := map[string]string{}
	if len(userIDs) == 0 {
		return emails, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email FROM users WHERE id = ANY($1)`, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		emails[id] = email
	}
	return emails, rows.Err()
}

// DeleteDeviceToken forgets a token — called when FCM reports it as
// unregistered, which is what an uninstalled app looks like from here.
func (s *Store) DeleteDeviceToken(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM device_tokens WHERE token = $1`, token)
	return err
}

type registerDeviceRequest struct {
	Token string `json:"token"`
	// Platform is informational — "android", "ios", … — kept so a future
	// channel can tell devices apart without a second round of migrations.
	Platform string `json:"platform"`
}

// registerDeviceHandler stores the push token the caller's app was issued.
//
// Idempotent by design: the mobile client re-registers on every launch and
// whenever the provider rotates the token, and has no way to know whether
// this one is already stored.
func registerDeviceHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}

		var req registerDeviceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "corps de requête invalide")
			return
		}

		token := strings.TrimSpace(req.Token)
		if token == "" {
			writeError(w, http.StatusBadRequest, "jeton d'appareil manquant")
			return
		}
		if len([]rune(token)) > maxDeviceTokenRunes {
			writeError(w, http.StatusBadRequest, "jeton d'appareil trop long")
			return
		}

		platform := strings.TrimSpace(req.Platform)
		if platform == "" {
			platform = "unknown"
		}

		if err := store.RegisterDeviceToken(r.Context(), claims.Subject, token, platform); err != nil {
			log.Printf("registering device token for %s: %v", claims.Subject, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
Bash Read updateTextHandler to hook the notification
IN
grep -n "func updateTextHandler" -A 45 texts.go | head -50

OUT
238:func updateTextHandler(repo *queel.Repository) http.HandlerFunc {
239-	return func(w http.ResponseWriter, r *http.Request) {
240-		if !requirePermission(w, r, rbac.ActionUpdateText) {
241-			return
242-		}
243-
244-		id := r.PathValue("id")
245-
246-		title, content, ok := decodeTextPayload(w, r)
247-		if !ok {
248-			return
249-		}
250-
251-		text, err := repo.UpdateText(id, title, content)
252-		if err != nil {
253-			if errors.Is(err, queel.ErrNotFound) {
254-				writeError(w, http.StatusNotFound, "texte introuvable")
255-				return
256-			}
257-			writeError(w, http.StatusInternalServerError, "erreur serveur")
258-			return
259-		}
260-
261-		writeJSON(w, http.StatusOK, textResponse{ID: text.ID})
262-	}
263-}
264-
265-// deleteTextHandler removes a single text outright — see
266-// queel.Repository.DeleteText for exactly what that cascades to (its
267-// rounds/fragments/votes, but not any text it was later forked into).
268-// Gated on the same right as creating a text in the first place
269-// (rbac.ActionCreateText) rather than a dedicated permission bit — whoever
270-// can add a text to the corpus can also remove one.
271-func deleteTextHandler(repo *queel.Repository, index *searchIndexer) http.HandlerFunc {
272-	return func(w http.ResponseWriter, r *http.Request) {
273-		claims, ok := claimsFromContext(r)
274-		if !ok || !claims.Allows(rbac.ActionCreateText) {
275-			writeError(w, http.StatusForbidden, "droits insuffisants")
276-			return
277-		}
278-
279-		id := r.PathValue("id")
280-
281-		deleteErr := repo.DeleteText(id)
282-		if deleteErr != nil && !errors.Is(deleteErr, queel.ErrNotFound) {
283-			writeError(w, http.StatusInternalServerError, "erreur serveur")
Write notifications.go
140 lines
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/etouraille/queel"
	"vote-api/notify"
)

// notifyTimeout bounds a fan-out. It runs detached from the request that
// triggered it (see textNotifier.TextUpdated), so nothing else would ever
// stop it.
const notifyTimeout = 30 * time.Second

// textNotifier turns "this text changed" into "these people should hear
// about it", and hands the result to the channels.
//
// It sits between queel (who follows what) and the api's own Postgres
// store (how to reach them) precisely because neither knows about the
// other: subscriptions live in queel, email addresses and device tokens
// live in Postgres.
type textNotifier struct {
	repo       *queel.Repository
	store      *Store
	dispatcher *notify.Dispatcher
}

func newTextNotifier(repo *queel.Repository, store *Store, dispatcher *notify.Dispatcher) *textNotifier {
	return &textNotifier{repo: repo, store: store, dispatcher: dispatcher}
}

// TextUpdated notifies everyone following text, except actorID — whoever
// made the change does not need telling, and since CreateText subscribes
// an author to their own text, skipping this would notify them of every
// edit they make themselves.
//
// Returns immediately: delivery runs in the background on its own context,
// so a slow provider never delays the response to the edit that triggered
// it, and cancelling that request doesn't cancel the notification.
func (n *textNotifier) TextUpdated(text *queel.Text, actorID string) {
	if n == nil || n.dispatcher == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()

		recipients, err := n.recipients(ctx, text.ID, actorID)
		if err != nil {
			log.Printf("notify: resolving recipients for text %s: %v", text.ID, err)
			return
		}

		n.dispatcher.Notify(ctx, notify.Notification{
			Title: "Texte modifié",
			Body:  fmt.Sprintf("« %s » vient d'être modifié.", text.Title),
			Data: map[string]string{
				"type":   "text.updated",
				"textId": text.ID,
			},
		}, recipients)
	}()
}

// recipients resolves the followers of textID into addressable recipients,
// dropping actorID.
func (n *textNotifier) recipients(ctx context.Context, textID, actorID string) ([]notify.Recipient, error) {
	subscribers, err := n.repo.SubscribersForText(textID)
	if err != nil {
		return nil, err
	}

	userIDs := make([]string, 0, len(subscribers))
	for _, userID := range subscribers {
		if userID != actorID {
			userIDs = append(userIDs, userID)
		}
	}
	if len(userIDs) == 0 {
		return nil, nil
	}

	// One query each rather than per recipient: a text with many followers
	// would otherwise cost two round trips per person.
	emails, err := n.store.EmailsForUsers(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	deviceTokens, err := n.store.DeviceTokensForUsers(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	recipients := make([]notify.Recipient, 0, len(userIDs))
	for _, userID := range userIDs {
		recipients = append(recipients, notify.Recipient{
			UserID:       userID,
			Email:        emails[userID],
			DeviceTokens: deviceTokens[userID],
		})
	}
	return recipients, nil
}

// buildDispatcher assembles the channels a deployment has actually
// configured. Every one of them is optional, and an unconfigured
// deployment gets the log channel alone — visible, inert, and impossible
// to mistake for a broken provider.
func buildDispatcher(store *Store, serviceAccountPath string) *notify.Dispatcher {
	channels := []notify.Channel{notify.LogChannel{}}

	push, err := notify.NewPushChannel(serviceAccountPath, func(token string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.DeleteDeviceToken(ctx, token); err != nil {
			log.Printf("notify: forgetting dead device token: %v", err)
		}
	})
	switch {
	case err != nil:
		// Misconfigured push is worth saying out loud, but not worth
		// refusing to start over: every other channel still works.
		log.Printf("notify: push channel disabled: %v", err)
	case push != nil:
		channels = append(channels, push)
	}

	if mailConfigured() {
		channels = append(channels, notify.NewEmailChannel(sendEmail))
	}

	dispatcher := notify.NewDispatcher(channels...)
	log.Printf("notify: channels enabled: %v", dispatcher.Channels())
	return dispatcher
}
Reste à brancher : la route, le déclenchement sur modification, et le câblage dans main.go.

Edit texts.go
Modified
Edit texts.go
Added 8 lines
Bash Locate wiring points in main.go
IN
grep -n "updateTextHandler\|GET /api/me/subscriptions" main.go

OUT
219:	mux.HandleFunc("PUT /api/texts/{id}", updateTextHandler(textRepo))
226:	mux.HandleFunc("GET /api/me/subscriptions", subscriptionsHandler(textRepo))
Edit main.go
Modified
Bash Find where to construct the notifier
IN
sed -n '186,196p' main.go

OUT
	var scheduledCloseLeaderCheck func() bool
	if clustered {
		scheduledCloseLeaderCheck = func() bool { return isScheduledCloseLeader(membership, self) }
	}
	go runScheduledCloseWorker(context.Background(), textRepo, searchIndex, scheduledCloseInterval, scheduledCloseLeaderCheck)

	mux := http.NewServeMux()
	// Outside the /api/... prefix on purpose — requireToken only gates that
	// prefix, so orchestration can probe this without a bearer token.
	mux.HandleFunc("GET /healthz", healthHandler(db, queelStore, membership))
	mux.HandleFunc("POST /api/auth/register", registerHandler(store))
Edit main.go
Added 5 lines
Edit main.go
Added 1 line
Bash Build, vet and test the API
IN
export PATH=/home/etouraille/go/bin:$PATH
cd /home/etouraille/src/vote/api
go build ./... 2>&1 | grep -v "^warning:" | head -10
go vet ./... 2>&1 | grep -v "^warning:" | head -10
go test ./ 2>&1 | grep -v "^warning:" | tail -3

OUT
ok  	vote-api	0.743s
Update Todos

queel : SubscribersForText + test

api : coeur multicanal (Channel, dispatcher, canaux log/email/push)

api : enregistrement des jetons d'appareil + route

api : déclenchement à la modification d'un texte, vers les abonnés

api : documentation .env.example

mobile : Firebase via .env + enregistrement du jeton + procédure Android documentée

Vérification : build, vet, tests, analyze
Bash Locate insertion point in .env.example
IN
grep -n "^GOOGLE_CLIENT_ID_MOBILE=" /home/etouraille/src/vote/api/.env.example

OUT
86:GOOGLE_CLIENT_ID_MOBILE=
Edit .env.example
Added 24 lines
Passons au mobile : paramètres Firebase dans le .env et procédure Android documentée.

Update Todos

queel : SubscribersForText + test

api : coeur multicanal (Channel, dispatcher, canaux log/email/push)

api : enregistrement des jetons d'appareil + route

api : déclenchement à la modification d'un texte, vers les abonnés

api : documentation .env.example

mobile : Firebase via .env + enregistrement du jeton + procédure Android documentée

Vérification : build, vet, tests, analyze
Bash Add Firebase messaging dependencies
IN
cd /home/etouraille/src/vote/mobile && flutter pub add firebase_core firebase_messaging flutter_local_notifications 2>&1 | tail -12

OUT
+ flutter_local_notifications_windows 3.1.1
  flutter_secure_storage_darwin 0.3.2 (0.4.0 available)
  hooks 2.0.2 (2.1.0 available)
  matcher 0.12.19 (0.12.20 available)
  meta 1.18.0 (1.19.0 available)
  record_use 0.6.0 (1.0.0 available)
  test_api 0.7.11 (0.7.13 available)
+ timezone 0.11.1
  vector_math 2.2.0 (2.4.1 available)
Changed 14 dependencies!
9 packages have newer versions incompatible with dependency constraints.
Try `flutter pub outdated` for more information.
Read .env
Edit .env
Added 61 lines
Edit env.dart
Added 17 lines
Write device_api.dart
16 lines
import '../../../../core/api/api_client.dart';
import '../../../../core/api/endpoints.dart';

class DeviceApi {
  DeviceApi._();

  /// Tells the api this device can be reached at [token].
  ///
  /// Called on every launch and again whenever FCM rotates the token: the
  /// app has no way of knowing whether the server already has it, and the
  /// route is idempotent precisely so it doesn't need to.
  static Future<void> register(String token, {String platform = 'android'}) async {
    await ApiClient.post(Endpoints.devices, {'token': token, 'platform': platform});
  }
}
Edit endpoints.dart
Added 3 lines
Write notification_service.dart
122 lines
import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../../app/config/env.dart';
import 'data/datasources/device_api.dart';

/// Android notification channel. Android groups notifications by channel
/// and lets the user mute them per channel, so one is declared explicitly
/// rather than falling back to the unnamed default.
const _androidChannel = AndroidNotificationChannel(
  'text_updates',
  'Modifications de textes',
  description: 'Notifie les textes que vous suivez lorsqu\'ils sont modifiés.',
  importance: Importance.high,
);

/// Sets up push notifications and keeps the api's copy of this device's
/// token current.
///
/// Everything here is best-effort by design: notifications are an extra,
/// and no failure in this file may keep the app from starting. Each step
/// logs and gives up rather than throwing.
class NotificationService {
  NotificationService._();

  static final _localNotifications = FlutterLocalNotificationsPlugin();

  /// Called once at startup, before runApp. Does nothing at all when the
  /// Firebase settings are absent from .env — an app built without
  /// notifications configured must still run normally.
  static Future<void> initialize() async {
    if (!Env.pushConfigured) return;

    try {
      await Firebase.initializeApp(
        // Options passed explicitly rather than read from a
        // google-services.json, so the whole configuration lives in .env
        // and the Gradle plugin isn't needed. See that file for where each
        // value comes from.
        options: FirebaseOptions(
          apiKey: Env.firebaseApiKey,
          appId: Env.firebaseAppId,
          messagingSenderId: Env.firebaseMessagingSenderId,
          projectId: Env.firebaseProjectId,
        ),
      );

      await _setUpLocalNotifications();

      // Android 13+ won't display anything without this. A refusal only
      // costs the display: the token is still registered, so the server
      // side stays consistent either way.
      await FirebaseMessaging.instance.requestPermission();

      // A message that arrives while the app is in the foreground is
      // delivered to the app instead of being shown by the system, so it
      // has to be surfaced by hand or it goes unnoticed.
      FirebaseMessaging.onMessage.listen(_showForegroundNotification);
    } catch (error) {
      debugPrint('notifications: initialisation impossible: $error');
    }
  }

  static Future<void> _setUpLocalNotifications() async {
    await _localNotifications.initialize(
      const InitializationSettings(
        android: AndroidInitializationSettings('@mipmap/ic_launcher'),
      ),
    );
    await _localNotifications
        .resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>()
        ?.createNotificationChannel(_androidChannel);
  }

  static Future<void> _showForegroundNotification(RemoteMessage message) async {
    final notification = message.notification;
    if (notification == null) return;

    await _localNotifications.show(
      notification.hashCode,
      notification.title,
      notification.body,
      NotificationDetails(
        android: AndroidNotificationDetails(
          _androidChannel.id,
          _androidChannel.name,
          channelDescription: _androidChannel.description,
          importance: Importance.high,
philosophy:        ),
      ),
    );
  }

  /// Registers this device against the signed-in user, and keeps doing so
  /// whenever FCM rotates the token.
  ///
  /// Must run *after* sign-in, not at startup: the api takes the owner from
  /// the bearer token, so registering without a session would be rejected.
  static Future<void> registerDevice() async {
    if (!Env.pushConfigured) return;

    try {
      final token = await FirebaseMessaging.instance.getToken();
      if (token != null) await DeviceApi.register(token);

      // FCM replaces a token on reinstall, restore, or when it expires;
      // without this the server would keep pushing to a dead one.
      FirebaseMessaging.instance.onTokenRefresh.listen((token) async {
        try {
          await DeviceApi.register(token);
        } catch (error) {
          debugPrint('notifications: enregistrement du jeton renouvelé impossible: $error');
        }
      });
    } catch (error) {
      debugPrint('notifications: enregistrement du jeton impossible: $error');
    }
  }
}
Une coquille s'est glissée dans mon fichier (philosophy:), et l'API de flutter_local_notifications diffère de ce que j'ai supposé. Je vérifie les signatures réelles.

Bash Check flutter_local_notifications API signatures
IN
grep -n "flutter_local_notifications:" -A1 /home/etouraille/src/vote/mobile/pubspec.yaml | head -3
P=$(ls -d /home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-* 2>/dev/null | tail -1); echo "PKG=$P"
grep -n "Future<bool?> initialize\|Future<void> show" -A 8 $P/lib/flutter_local_notifications.dart | head -30

OUT
47:  flutter_local_notifications: ^22.2.0
48-
PKG=/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0
Bash Read the plugin API for v22
IN
P=/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0
grep -rn "initialize({\|show({" -A 10 $P/lib/src/flutter_local_notifications_plugin.dart | head -40

OUT
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart:111:  Future<bool?> initialize({
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-112-    required InitializationSettings settings,
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-113-    DidReceiveNotificationResponseCallback? onDidReceiveNotificationResponse,
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-114-    DidReceiveBackgroundNotificationResponseCallback?
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-115-    onDidReceiveBackgroundNotificationResponse,
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-116-  }) async {
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-117-    if (kIsWeb) {
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-118-      return resolvePlatformSpecificImplementation<
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-119-            WebFlutterLocalNotificationsPlugin
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-120-          >()
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-121-          ?.initialize(
--
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart:252:  Future<void> show({
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-253-    required int id,
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-254-    String? title,
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-255-    String? body,
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-256-    NotificationDetails? notificationDetails,
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-257-    String? payload,
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-258-  }) async {
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-259-    if (kIsWeb) {
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-260-      await resolvePlatformSpecificImplementation<
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-261-            WebFlutterLocalNotificationsPlugin
/home/etouraille/.pub-cache/hosted/pub.dev/flutter_local_notifications-22.2.0/lib/src/flutter_local_notifications_plugin.dart-262-          >()
Edit notification_service.dart
Modified
Edit notification_service.dart
Modified
Reste à brancher l'initialisation au démarrage et l'enregistrement du jeton après connexion.

Edit main.dart
Added 10 lines
Edit main.dart
Added 3 lines
Edit login_page.dart
Added 4 lines
Edit login_page.dart
Added 2 lines
Edit login_page.dart
Added 1 line
Bash Analyze the mobile app
IN
flutter analyze 2>&1 | tail -12

OUT
Analyzing mobile...                                             
No issues found! (ran in 2.5s)
Bash Run tests and build the APK with Firebase
IN
flutter test 2>&1 | tail -3 && flutter build apk --debug 2>&1 | tail -6

OUT
00:00 +0: loading /home/etouraille/src/vote/mobile/test/widget_test.dart
00:00 +0: shows the login page when no session is stored
00:00 +1: All tests passed!
> Run with --scan to generate a Build Scan (Powered by Develocity).
> Get more help at https://help.gradle.org.

BUILD FAILED in 34s
Running Gradle task 'assembleDebug'...                             35,2s
Gradle task assembleDebug failed with exit code 1
Bash Extract the Gradle build failure
IN
flutter build apk --debug 2>&1 | grep -iE "error|minSdk|Caused|required|What went wrong" -A 3 | head -30

OUT
* What went wrong:
Execution failed for task ':app:checkDebugAarMetadata'.
> A failure occurred while executing com.android.build.gradle.internal.tasks.CheckAarMetadataWorkAction
   > An issue was found when checking AAR metadata:
Bash Read the AAR metadata conflict detail
IN
flutter build apk --debug 2>&1 | grep -A 12 "An issue was found when checking AAR metadata" | head -20

OUT
   > An issue was found when checking AAR metadata:

       1.  Dependency ':flutter_local_notifications' requires core library desugaring to be enabled
           for :app.

           See https://developer.android.com/studio/write/java8-support.html for more
           details.

* Try:
> Run with --stacktrace option to get the stack trace.
> Run with --info or --debug option to get more log output.
> Run with --scan to generate a Build Scan (Powered by Develocity).
> Get more help at https://help.gradle.org.
flutter_local_notifications exige le core library desugaring. Je l'active.

Edit build.gradle.kts
Added 4 lines
Edit build.gradle.kts
Added 6 lines
Bash Rebuild the APK after enabling desugaring
IN
flutter build apk --debug 2>&1 | tail -4

OUT
Running Gradle task 'assembleDebug'...                             65,1s
✓ Built build/app/outputs/flutter-apk/app-debug.apk
Bash Full Go verification across both modules
IN
export PATH=/home/etouraille/go/bin:$PATH
cd /home/etouraille/src/vote/api
echo "=== api ==="; go build ./... 2>&1 | grep -v "^warning:" | head -3; go vet ./... 2>&1 | grep -v "^warning:" | head -3; go test ./ 2>&1 | grep -v "^warning:" | tail -2
echo "=== queel ==="; cd queel && go build ./... 2>&1 | grep -v "^warning:" | head -3; go vet ./... 2>&1 | grep -v "^warning:" | head -3; go test ./... 2>&1 | grep -v "^warning:" | grep -vE "no test files" | tail -4

OUT
=== api ===
ok  	vote-api	(cached)
=== queel ===
ok  	github.com/etouraille/queel/cluster	0.209s
ok  	github.com/etouraille/queel/merkle	(cached)
ok  	github.com/etouraille/queel/rbac	1.177s
ok  	github.com/etouraille/queel/server	0.012s
Bash Confirm queel root package and mobile checks
IN
export PATH=/home/etouraille/go/bin:$PATH
go test ./ 2>&1 | grep -v "^warning:" | tail -2
cd /home/etouraille/src/vote/mobile && flutter analyze 2>&1 | tail -2 && flutter test 2>&1 | tail -1

OUT
ok  	github.com/etouraille/queel	(cached)
Analyzing mobile...                                             
No issues found! (ran in 2.7s)
00:00 +1: All tests passed!
Update Todos

queel : SubscribersForText + test

api : coeur multicanal (Channel, dispatcher, canaux log/email/push)

api : enregistrement des jetons d'appareil + route

api : déclenchement à la modification d'un texte, vers les abonnés

api : documentation .env.example

mobile : Firebase via .env + enregistrement du jeton + procédure Android documentée

Vérification : build, vet, tests, analyze
Le système est en place et tout compile.

Le cœur multicanal — api/notify/
Une interface Channel (Name, Send), et un Dispatcher qui diffuse en parallèle. Trois principes structurent le paquet :

Les canaux sont indépendants. L'un qui échoue n'arrête pas les autres, et Notify ne renvoie aucune erreur : aucun appelant ne saurait quoi faire de « le canal email était en panne », et la remonter inviterait à faire échouer la modification de texte pour ça. Les échecs sont journalisés.
Un canal reçoit toute la liste d'un coup, pas un destinataire à la fois, pour qu'un transport capable de grouper puisse le faire.
Ne pas pouvoir joindre quelqu'un n'est pas une erreur. Un utilisateur sans appareil enregistré n'est simplement pas joignable par le canal push.
Trois canaux, tous optionnels et activés par la seule présence de leur configuration :

Canal	Condition	Rôle
log	toujours	trace qui aurait été notifié
push	FCM_SERVICE_ACCOUNT_FILE	FCM HTTP v1
email	BREVO_API_KEY + MAIL_FROM	réutilise ton client Brevo
Le canal log n'est pas décoratif : sans lui, « les notifications ne sont pas configurées » et « les notifications sont cassées » sont indiscernables, et cette ambiguïté coûte bien plus cher à diagnostiquer que ce canal ne coûte à garder.

FCM v1 exige un jeton OAuth2 signé par le compte de service. Je l'ai implémenté à la main (fcm_token.go) — signature RS256, échange JWT-bearer, cache jusqu'à expiration — plutôt que d'ajouter golang.org/x/oauth2 à un module qui compte trois dépendances. v1 n'ayant pas de multicast, l'envoi est parallélisé mais borné à 8 requêtes simultanées. Un jeton rejeté en 404 est oublié en base : une app désinstallée laisse sinon son jeton à vie.

Le ciblage
SubscribersForText dans queel — l'image miroir de SubscriptionsForUser, gratuite puisque la clé primaire commence déjà par l'identifiant du texte.

Le test m'a appris quelque chose que j'ignorais : CreateText abonne d'office l'auteur à son propre texte. Sans cela, un auteur se notifierait lui-même à chaque édition. La diffusion exclut donc l'acteur, et j'ai figé ce comportement dans un test pour qu'il ne se perde pas.

Le déclenchement
updateTextHandler notifie après l'écriture réussie, dans une goroutine détachée avec son propre contexte : un fournisseur lent ne retarde jamais la réponse, et annuler la requête n'annule pas la notification.

Côté mobile
Les quatre paramètres Firebase sont dans lib/config/.env, avec la procédure Android complète en six étapes — création du projet, application Android avec com.queel.mobile et la SHA-1, où lire chaque valeur, clé de compte de service, migration SQL, redémarrage. J'y ai aussi noté le piège d'Android 13+ : POST_NOTIFICATIONS doit être accordée pour afficher, mais un refus n'empêche pas l'enregistrement du jeton.

Choix notable : Firebase.initializeApp reçoit des FirebaseOptions construites depuis le .env, sans google-services.json ni plugin Gradle. Toute la configuration reste au même endroit, comme tu l'as demandé. Ces valeurs ne sont pas secrètes — un google-services.json est de toute façon en clair dans l'APK ; ce qui protège l'envoi, c'est la clé privée, qui reste côté API.

L'enregistrement du jeton se fait après connexion, jamais avant : l'API lit le propriétaire dans le jeton de session.

Vérifié : go build, go vet, suites vote-api et queel complètes au vert ; flutter analyze propre, test vert, APK construit. Ce dernier a d'abord échoué — flutter_local_notifications réclame le core library desugaring, que j'ai activé dans build.gradle.kts.

Trois choses à faire avant que ça fonctionne
Renseigner les quatre valeurs Firebase et FCM_SERVICE_ACCOUNT_FILE — je n'ai pas de projet Firebase, donc rien n'est encore configuré.
Appliquer 005_create_device_tokens.sql à la main : migrations/ n'est joué qu'à la première initialisation du conteneur Postgres, jamais sur une base existante.
Redémarrer API et cluster avec les binaires reconstruits.
Et une réserve honnête : je n'ai exercé aucun de ces chemins à l'exécution. Sans projet Firebase, ni l'obtention du jeton, ni l'envoi FCM, ni l'affichage n'ont tourné une seule fois. Le canal log est en revanche testable immédiatement — modifie un texte suivi par quelqu'un d'autre, la ligne notify(log): doit apparaître dans les journaux de l'API avec la liste des destinataires. C'est le premier essai que je te conseille.

Je n'ai pas répercuté POST /api/me/devices dans queel/server : les jetons vivent en Postgres, pas dans queel.Repository, exactement comme les routes d'authentification que ton 8080.md écarte pour la même raison.