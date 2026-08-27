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
			// Web browsers ignore the android block above and read this
			// one. A token is a token to FCM v1 — the same request reaches
			// a phone or a tab depending only on where the token came from
			// — but the per-platform blocks decide how it is rendered.
			"webpush": map[string]any{
				"headers": map[string]string{
					// Without it a browser may collapse several
					// notifications into one; "high" keeps each event its
					// own line in the tray.
					"Urgency": "high",
				},
				"fcm_options": map[string]string{
					// Where a click lands when no tab is open. An open tab
					// is focused and routed by event type instead (see the
					// front's firebase-messaging-sw.js).
					"link": "/notifications",
				},
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
