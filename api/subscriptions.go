package main

import (
	"errors"
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
