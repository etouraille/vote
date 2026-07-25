// Package server exposes a queel.Repository over a small, self-contained
// JSON HTTP API. Callers identify themselves purely via the
// authorId/userId fields they pass in, exactly as when calling the
// Repository directly — server never derives identity from a token. Any
// project — this one or another — talks to it only through the client
// package, over the network, never by importing the engine or repository
// directly.
//
// Authorization is separate from identity and optional: pass a non-empty
// jwtSecret to NewHandler and every mutating route additionally requires a
// bearer JWT (see queel/rbac) whose claims allow the corresponding
// rbac.Action. Pass nil to leave the server exactly as unauthenticated as
// it always was — e.g. for embedded/test use where the caller already
// trusts every request it receives.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/rbac"
)

// NewHandler builds the HTTP handler for repo's text/round/fragment/vote
// operations. See the package doc for what jwtSecret does.
func NewHandler(repo *queel.Repository, jwtSecret []byte) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /texts", createTextHandler(repo, jwtSecret))
	mux.HandleFunc("GET /texts", recentTextsHandler(repo))
	mux.HandleFunc("GET /texts/{id}", getTextHandler(repo))
	mux.HandleFunc("GET /texts/{id}/with-slots", textWithSlotsHandler(repo))
	mux.HandleFunc("PUT /texts/{id}", updateTextHandler(repo, jwtSecret))
	mux.HandleFunc("POST /texts/{id}/propose-edit", proposeEditHandler(repo, jwtSecret))
	mux.HandleFunc("GET /texts/{id}/round", currentRoundHandler(repo))
	mux.HandleFunc("POST /texts/{id}/close-round", closeRoundHandler(repo, jwtSecret))
	mux.HandleFunc("GET /texts/{id}/slots/{slotId}/fragments", fragmentsHandler(repo))
	mux.HandleFunc("GET /fragments/{id}", getFragmentHandler(repo))
	mux.HandleFunc("POST /vote", castVoteHandler(repo, jwtSecret))
	mux.HandleFunc("GET /fragments/{id}/votes", voteCountHandler(repo))

	return mux
}

// checkAction reports whether the request may proceed, writing a 401/403
// response itself and returning false if not. When jwtSecret is empty,
// authorization is disabled entirely and every request is allowed through,
// preserving server's original no-auth behavior.
func checkAction(w http.ResponseWriter, r *http.Request, jwtSecret []byte, action rbac.Action) bool {
	if len(jwtSecret) == 0 {
		return true
	}

	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}

	claims, err := rbac.VerifyToken(token, jwtSecret)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return false
	}
	if !claims.Allows(action) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return false
	}
	return true
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeRepositoryError maps the Repository's sentinel errors to HTTP status
// codes; anything else (validation failures, e.g. an invalid or overlapping
// range) is treated as a 400.
func writeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, queel.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, queel.ErrNoOpenRound):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

type createTextRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func createTextHandler(repo *queel.Repository, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAction(w, r, jwtSecret, rbac.ActionCreateText) {
			return
		}

		var req createTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		text, err := repo.CreateText(req.Title, req.Content)
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, text)
	}
}

// defaultRecentTextsLimit and maxRecentTextsLimit bound the ?limit= query
// param on GET /texts — a sane default when it's omitted, and a ceiling so
// a caller can't force an unbounded full-corpus scan.
const (
	defaultRecentTextsLimit = 20
	maxRecentTextsLimit     = 100
)

func recentTextsHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := defaultRecentTextsLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeError(w, http.StatusBadRequest, "invalid limit")
				return
			}
			limit = parsed
		}
		if limit > maxRecentTextsLimit {
			limit = maxRecentTextsLimit
		}

		texts, err := repo.RecentTexts(limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, texts)
	}
}

func getTextHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		text, err := repo.Text(r.PathValue("id"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, text)
	}
}

// textWithSlotsHandler joins a text with the slots of its current round —
// see queel.Repository.TextWithSlots. No round open isn't an error: it's a
// 200 with roundNumber 0 and an empty slots list.
func textWithSlotsHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := repo.TextWithSlots(r.PathValue("id"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

type updateTextRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// updateTextHandler overwrites a text's title/content directly, bypassing
// the voting workflow entirely — see rbac.ActionUpdateText.
func updateTextHandler(repo *queel.Repository, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAction(w, r, jwtSecret, rbac.ActionUpdateText) {
			return
		}

		var req updateTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		text, err := repo.UpdateText(r.PathValue("id"), req.Title, req.Content)
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, text)
	}
}

type proposeEditRequest struct {
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Content  string `json:"content"`
	AuthorID string `json:"authorId"`
}

// proposeEditHandler requires rbac.ActionSelect if [start,end) isn't
// already an open slot in the text's current round, or the less
// consequential rbac.ActionEditSelection if it is — mirroring the
// distinction rbac.Permissions itself documents between CanSelect and
// CanEditSelection.
func proposeEditHandler(repo *queel.Repository, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		textID := r.PathValue("id")

		var req proposeEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if len(jwtSecret) > 0 {
			action := rbac.ActionSelect
			if round, err := repo.CurrentRound(textID); err == nil {
				for _, slot := range round.Slots {
					if slot.Start == req.Start && slot.End == req.End {
						action = rbac.ActionEditSelection
						break
					}
				}
			} else if !errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if !checkAction(w, r, jwtSecret, action) {
				return
			}
		}

		fragment, err := repo.ProposeEdit(textID, req.Start, req.End, req.Content, req.AuthorID)
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, fragment)
	}
}

func currentRoundHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		round, err := repo.CurrentRound(r.PathValue("id"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, round)
	}
}

func closeRoundHandler(repo *queel.Repository, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAction(w, r, jwtSecret, rbac.ActionCloseText) {
			return
		}

		outcome, err := repo.CloseRound(r.PathValue("id"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	}
}

func fragmentsHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fragments, err := repo.Fragments(r.PathValue("id"), r.PathValue("slotId"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, fragments)
	}
}

func getFragmentHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fragment, err := repo.Fragment(r.PathValue("id"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, fragment)
	}
}

type castVoteRequest struct {
	FragmentID string `json:"fragmentId"`
	UserID     string `json:"userId"`
}

func castVoteHandler(repo *queel.Repository, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAction(w, r, jwtSecret, rbac.ActionVote) {
			return
		}

		var req castVoteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := repo.CastVote(req.FragmentID, req.UserID); err != nil {
			writeRepositoryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func voteCountHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := repo.VoteCount(r.PathValue("id"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"votes": count})
	}
}
