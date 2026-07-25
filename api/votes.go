package main

import (
	"errors"
	"net/http"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/rbac"
)

// getFragmentHandler fetches a single fragment by ID — a read, so no
// permission gate, same as getTextHandler/textWithSlotsHandler.
func getFragmentHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fragment, err := repo.Fragment(r.PathValue("id"))
		if err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "fragment introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}
		writeJSON(w, http.StatusOK, fragment)
	}
}

// castVoteHandler records the caller as voting for a fragment. The voter is
// always the authenticated caller (claims.Subject) — unlike queeld's own
// HTTP layer, which trusts a client-supplied userId field, the api never
// lets a caller vote as anyone but themselves.
func castVoteHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePermission(w, r, rbac.ActionVote) {
			return
		}
		claims, _ := claimsFromContext(r)

		if err := repo.CastVote(r.PathValue("id"), claims.Subject); err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "fragment introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
