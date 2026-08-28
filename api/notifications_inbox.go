package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/etouraille/queel"
	"github.com/lib/pq"
)

// defaultNotificationLimit / maxNotificationLimit bound a listing. An inbox
// is read newest-first and nobody scrolls back forever; the cap mostly
// exists so a client can't ask for the whole table.
const (
	defaultNotificationLimit = 50
	maxNotificationLimit     = 200

	// latestRoundOverfetch is how much wider than the requested page the
	// listing reads, to leave room for what keepLatestRound removes. A
	// reader whose inbox is mostly about superseded versions would
	// otherwise get a page far shorter than they asked for.
	//
	// A multiplier and not a loop: this is a reading list, not a
	// guarantee, and one over-wide read beats an unbounded chase after
	// exactly `limit` rows.
	latestRoundOverfetch = 3
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

// UnreadNotificationTexts returns the text each unread notification is
// about, one entry per notification — the empty string for one that
// concerns no text in particular.
//
// The ids rather than a count, because what a badge should show is decided
// after the same filter the listing applies (see keepLatestRound). Counting
// in SQL would count rows the list then hides, and the badge would promise
// notifications nobody can find.
func (s *Store) UnreadNotificationTexts(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT coalesce(text_id, '') FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	textIDs := make([]string, 0)
	for rows.Next() {
		var textID string
		if err := rows.Scan(&textID); err != nil {
			return nil, err
		}
		textIDs = append(textIDs, textID)
	}
	return textIDs, rows.Err()
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
// keepLatestRound drops the notifications that concern a version of a text
// a later round has already superseded.
//
// Each version of a text carries exactly one round, and closing it forks a
// new version with a new id (see queel's CloseRound). "The latest round of
// a text" is therefore "the version nothing has superseded" — no round
// number has to be recorded anywhere for this, the fork chain already says
// it.
//
// One lookup per distinct text rather than per notification: an inbox is
// usually several events about a handful of texts.
//
// Notifications about no text at all are kept: there is no round for them
// to be behind.
func keepLatestRound(repo *queel.Repository, textIDs []string) (map[string]bool, error) {
	current := make(map[string]bool, len(textIDs))
	for _, textID := range textIDs {
		if textID == "" || current[textID] {
			continue
		}
		superseded, err := repo.IsSuperseded(textID)
		if err != nil {
			return nil, err
		}
		current[textID] = !superseded
	}
	return current, nil
}

func listNotificationsHandler(store *Store, repo *queel.Repository) http.HandlerFunc {
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

		// Asked for more than will be shown: what the filter below drops
		// would otherwise leave the page short of the limit for no reason
		// the reader can see.
		stored, err := store.ListNotifications(r.Context(), claims.Subject, limit*latestRoundOverfetch)
		if err != nil {
			log.Printf("listing notifications for %s: %v", claims.Subject, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		unreadTexts, err := store.UnreadNotificationTexts(r.Context(), claims.Subject)
		if err != nil {
			log.Printf("counting unread notifications for %s: %v", claims.Subject, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		textIDs := make([]string, 0, len(stored)+len(unreadTexts))
		for _, notification := range stored {
			textIDs = append(textIDs, notification.TextID)
		}
		textIDs = append(textIDs, unreadTexts...)

		current, err := keepLatestRound(repo, textIDs)
		if err != nil {
			log.Printf("resolving the current version of a notified text for %s: %v", claims.Subject, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		// Never nil: an empty inbox has to marshal as [] rather than null.
		notifications := make([]storedNotification, 0, len(stored))
		for _, notification := range stored {
			if notification.TextID != "" && !current[notification.TextID] {
				continue
			}
			if len(notifications) == limit {
				break
			}
			notifications = append(notifications, notification)
		}

		// Counted after the same filter, or the badge would promise
		// notifications the list cannot show.
		unread := 0
		for _, textID := range unreadTexts {
			if textID == "" || current[textID] {
				unread++
			}
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
