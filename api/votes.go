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
func castVoteHandler(repo *queel.Repository, notifier *textNotifier) http.HandlerFunc {
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
		// Every follower of the text hears about it, bar the voter — being
		// told of your own vote is noise, and creating a text subscribes
		// you to it, so without this an author would be notified of each
		// of their own.
		notifier.VoteCast(r.PathValue("id"), claims.Subject)

		w.WriteHeader(http.StatusNoContent)
	}
}

// myVotesHandler returns which fragment the caller currently has voted for
// in each slot of a text, keyed by slot id. Slots they haven't voted in are
// absent.
//
// Its reason for existing is that a vote outlives the page that cast it:
// queel has always recorded the choice (see Repository.CastVote), but with
// no way to read it back, a client could only ever highlight a vote made
// during the current visit and forgot it on reload.
//
// Scoped to the caller like the other /me-style reads — the owner comes
// from the bearer token, never from the request — so nobody can enumerate
// how somebody else voted. Deliberately not gated by ActionVote either:
// reading your own past choices isn't voting, and someone whose right to
// vote was revoked should still see what they already voted for.
func myVotesHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}

		votes, err := repo.UserVotes(r.PathValue("id"), claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		// Never nil: an empty map has to marshal as {} rather than null.
		writeJSON(w, http.StatusOK, votes)
	}
}
