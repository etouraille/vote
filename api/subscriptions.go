package main

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/rbac"
)

type subscribeResponse struct {
	Subscribed bool `json:"subscribed"`
}

// subscribeHandler lets the caller follow a text — see queel.Subscription's
// doc comment: a personal "focus" signal rather than a right over the text
// itself. It is gated all the same (rbac.ActionSubscribe), because
// following is what surfaces a text's vote/edit/close actions in both front
// ends and what puts someone on the notification fan-out — so this is the
// single bit that decides whether an account takes part at all or stays a
// reader.
//
// Listing one's own subscriptions stays ungated below: withholding the
// right to follow new texts shouldn't hide the ones already followed.
func subscribeHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}
		if !requirePermission(w, r, rbac.ActionSubscribe) {
			return
		}

		textID := r.PathValue("id")
		if _, err := repo.Subscribe(claims.Subject, textID); err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "texte introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		writeJSON(w, http.StatusOK, subscribeResponse{Subscribed: true})
	}
}

// unsubscribeHandler stops the caller following a text.
//
// Ungated, unlike subscribing: taking back your own attention is not an
// action anyone should need a right for, and someone whose canSubscribe
// was revoked must still be able to leave the texts they had joined —
// otherwise a withdrawn permission would trap them in their subscriptions
// rather than stop them making new ones.
//
// Leaving also empties the inbox of what the text had already sent. The
// fan-out stops at once on its own — recipients are resolved per event —
// but the entries written while the reader was still following would
// otherwise stay in their list for good, about a text they have
// deliberately left.
func unsubscribeHandler(repo *queel.Repository, store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}

		textID := r.PathValue("id")
		if err := repo.Unsubscribe(claims.Subject, textID); err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		forgetNotifications(r.Context(), repo, store, claims.Subject, textID)
		writeJSON(w, http.StatusOK, subscribeResponse{Subscribed: false})
	}
}

// forgetNotifications clears the inbox of everything one text has sent a
// reader, across every version of it.
//
// The whole chain and not just the id asked for: a text is forked on each
// closed round, so its earlier notifications carry the ids of earlier
// versions. Leaving "the text" has to mean the text, not the version that
// happens to be current — even though the listing already hides the older
// ones (see keepLatestRound), because rows nobody can ever see again are
// not rows worth keeping.
//
// Never fails the request. Leaving is what was asked for and it has
// already succeeded; an inbox that keeps a few entries is a far smaller
// wrong than a 500 that suggests the reader is still subscribed.
func forgetNotifications(ctx context.Context, repo *queel.Repository, store *Store, userID, textID string) {
	if store == nil {
		return
	}

	textIDs := []string{textID}
	chain, err := repo.TextChain(textID)
	if err != nil {
		// The text itself may be gone — subscriptions outlive it. The
		// current id alone is still worth clearing.
		log.Printf("unsubscribe: reading the versions of text %s: %v", textID, err)
	} else {
		textIDs = textIDs[:0]
		for _, version := range chain {
			textIDs = append(textIDs, version.ID)
		}
	}

	if _, err := store.DeleteNotificationsForTexts(ctx, userID, textIDs); err != nil {
		log.Printf("unsubscribe: clearing %s's notifications about text %s: %v", userID, textID, err)
	}
}

// subscribedText is deliberately narrower than queel.Text: this feeds a
// list of titles (see the mobile app's "Mes abonnements"), and carrying
// every followed text's full content just to render its title would grow
// the response with the one field nobody displays.
type subscribedText struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// subscriptionsHandler lists the texts the caller follows, newest
// subscription first — the order SubscriptionsForUser already returns them
// in.
//
// A text that has since been deleted is skipped rather than failing the
// whole listing: subscriptions outlive the text they point at (nothing
// prunes them on delete), so one dangling id must not cost the caller
// every other subscription they have.
func subscriptionsHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}

		textIDs, err := repo.SubscriptionsForUser(claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		// Never nil: an empty list has to marshal as [] rather than null,
		// so clients can iterate without a special case for "no
		// subscriptions yet".
		texts := make([]subscribedText, 0, len(textIDs))
		for _, id := range textIDs {
			text, err := repo.Text(id)
			if err != nil {
				if errors.Is(err, queel.ErrNotFound) {
					continue
				}
				writeError(w, http.StatusInternalServerError, "erreur serveur")
				return
			}
			texts = append(texts, subscribedText{ID: text.ID, Title: text.Title})
		}

		writeJSON(w, http.StatusOK, texts)
	}
}
