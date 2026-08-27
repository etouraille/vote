package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/lib/pq"
)

// defaultNotificationLimit / maxNotificationLimit bound a listing. An inbox
// is read newest-first and nobody scrolls back forever; the cap mostly
// exists so a client can't ask for the whole table.
const (
	defaultNotificationLimit = 50
	maxNotificationLimit     = 200
)

// storedNotification is one row of the inbox as clients see it. read is a
// boolean here even though the column is a timestamp: when it was read is
// of no use to any current caller, only whether.
type storedNotification struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	TextID    string `json:"textId,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	Read      bool   `json:"read"`
}

// SaveNotifications writes one row per recipient in a single statement —
// unnest expands the user id array into rows, so a text with fifty
// followers costs one round trip rather than fifty.
func (s *Store) SaveNotifications(ctx context.Context, userIDs []string, kind, textID, title, body string) error {
	if len(userIDs) == 0 {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notifications (user_id, type, text_id, title, body)
		SELECT unnest($1::text[]), $2, NULLIF($3, ''), $4, $5`,
		pq.Array(userIDs), kind, textID, title, body)
	return err
}

// ListNotifications returns a user's inbox, newest first.
func (s *Store) ListNotifications(ctx context.Context, userID string, limit int) ([]storedNotification, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, coalesce(text_id, ''), title, body, created_at, read_at IS NOT NULL
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Never nil: an empty inbox has to marshal as [] rather than null.
	notifications := make([]storedNotification, 0)
	for rows.Next() {
		var n storedNotification
		var createdAt time.Time
		if err := rows.Scan(&n.ID, &n.Type, &n.TextID, &n.Title, &n.Body, &createdAt, &n.Read); err != nil {
			return nil, err
		}
		n.CreatedAt = createdAt.Format(time.RFC3339)
		notifications = append(notifications, n)
	}
	return notifications, rows.Err()
}

// UnreadNotificationCount counts what a badge would show. Returned with
// every listing so a client never has to ask twice, and counted over the
// whole inbox rather than the page just returned.
func (s *Store) UnreadNotificationCount(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&count)
	return count, err
}

// SetNotificationRead marks one notification read or unread, scoped to its
// owner. Reports whether a row matched, so the caller can 404 rather than
// silently accept an id belonging to someone else.
func (s *Store) SetNotificationRead(ctx context.Context, userID string, id int64, read bool) (bool, error) {
	var readAt any
	if read {
		readAt = time.Now()
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = $1 WHERE id = $2 AND user_id = $3`, readAt, id, userID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// MarkAllNotificationsRead empties the badge in one call. Already-read rows
// are left alone rather than restamped, so "when did I read this" survives
// a bulk acknowledge.
func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID string) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type notificationsResponse struct {
	Notifications []storedNotification `json:"notifications"`
	Unread        int                  `json:"unread"`
}

// listNotificationsHandler returns the caller's inbox plus the unread
// count, in one call — a client showing a list almost always shows a badge
// beside it, and splitting them would guarantee the two disagree.
func listNotificationsHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}

		limit := defaultNotificationLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeError(w, http.StatusBadRequest, "limit invalide")
				return
			}
			limit = min(parsed, maxNotificationLimit)
		}

		notifications, err := store.ListNotifications(r.Context(), claims.Subject, limit)
		if err != nil {
			log.Printf("listing notifications for %s: %v", claims.Subject, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}
		unread, err := store.UnreadNotificationCount(r.Context(), claims.Subject)
		if err != nil {
			log.Printf("counting unread notifications for %s: %v", claims.Subject, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		writeJSON(w, http.StatusOK, notificationsResponse{Notifications: notifications, Unread: unread})
	}
}

type setReadRequest struct {
	Read bool `json:"read"`
}

// setNotificationReadHandler flips one notification between read and
// unread. Both directions through the same route, since "mark as unread"
// is the same operation with the opposite value — a client that can only
// ever mark read makes an inbox impossible to revisit.
func setNotificationReadHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}

		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "identifiant invalide")
			return
		}

		var req setReadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "corps de requête invalide")
			return
		}

		found, err := store.SetNotificationRead(r.Context(), claims.Subject, id, req.Read)
		if err != nil {
			log.Printf("marking notification %d for %s: %v", id, claims.Subject, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}
		if !found {
			// Covers both "no such notification" and "not yours" — telling
			// them apart would confirm the existence of someone else's.
			writeError(w, http.StatusNotFound, "notification introuvable")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// markAllNotificationsReadHandler acknowledges the whole inbox at once,
// the one bulk action a badge actually needs.
func markAllNotificationsReadHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}

		updated, err := store.MarkAllNotificationsRead(r.Context(), claims.Subject)
		if err != nil {
			log.Printf("marking all notifications read for %s: %v", claims.Subject, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		writeJSON(w, http.StatusOK, map[string]int64{"updated": updated})
	}
}
