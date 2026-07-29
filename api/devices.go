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
