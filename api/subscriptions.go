package main

import (
	"errors"
	"net/http"

	"github.com/etouraille/queel"
)

type subscribeResponse struct {
	Subscribed bool `json:"subscribed"`
}

// subscribeHandler lets the caller follow a text — see queel.Subscription's
// doc comment: it's a personal "focus" signal, not a permission grant, so
// unlike vote/edit/close/delete this isn't gated by any rbac.Action —
// anyone with a valid session can subscribe to any text they can see.
func subscribeHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
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
