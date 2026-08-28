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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	mux.HandleFunc("DELETE /texts/{id}", deleteTextHandler(repo, jwtSecret))
	mux.HandleFunc("POST /texts/{id}/propose-edit", proposeEditHandler(repo, jwtSecret))
	mux.HandleFunc("GET /texts/{id}/round", currentRoundHandler(repo))
	mux.HandleFunc("GET /texts/{id}/history", historyHandler(repo))
	mux.HandleFunc("POST /texts/{id}/close-round", closeRoundHandler(repo, jwtSecret))
	mux.HandleFunc("POST /texts/{id}/schedule-close", scheduleCloseHandler(repo, jwtSecret))
	mux.HandleFunc("POST /texts/{id}/subscribe", subscribeHandler(repo, jwtSecret))
	mux.HandleFunc("GET /users/{userId}/subscriptions", subscriptionsHandler(repo))
	mux.HandleFunc("GET /texts/{id}/slots/{slotId}/fragments", fragmentsHandler(repo))
	mux.HandleFunc("GET /texts/{id}/votes/{userId}", userVotesHandler(repo))
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
		// Naming the action costs nothing here — checkAction already has it
		// — and saves an embedder from guessing which of the six rights a
		// refusal was about.
		writeError(w, http.StatusForbidden, "insufficient permissions: "+string(action))
		return false
	}
	return true
}

// checkSubscription mirrors the api's own rule: a text is acted on by the
// people who follow it (see the api's requireSubscription).
//
// userID empty means the caller did not name themselves, and this package
// has no other way to know who they are — it never reads a token for
// identity, only for permissions. The check then passes, exactly as
// checkAction passes when no jwtSecret was configured: authorization here
// is something an embedder opts into, not something the module imposes.
func checkSubscription(w http.ResponseWriter, repo *queel.Repository, textID, userID string) bool {
	if userID == "" {
		return true
	}

	subscribed, err := repo.IsSubscribed(userID, textID)
	if err != nil {
		writeRepositoryError(w, err)
		return false
	}
	if !subscribed {
		writeError(w, http.StatusForbidden, "not subscribed to this text")
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
	var superseded *queel.ErrTextSuperseded
	switch {
	case errors.Is(err, queel.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, queel.ErrNoOpenRound), errors.Is(err, queel.ErrEmptyRound):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, queel.ErrOverlappingSlot):
		// A client mistake, not a server failure — it used to fall through
		// to the 500 below, which told the caller nothing and blamed the
		// wrong side.
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &superseded):
		// supersededBy names the current version — enough for a caller to
		// retry there directly instead of just getting a dead end.
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":        err.Error(),
			"supersededBy": superseded.SupersededBy,
		})
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

type createTextRequest struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	AuthorID string `json:"authorId"`
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
		text, err := repo.CreateText(req.Title, req.Content, req.AuthorID)
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

		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				writeError(w, http.StatusBadRequest, "invalid offset")
				return
			}
			offset = parsed
		}

		texts, err := repo.RecentTexts(limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// userId is optional here, unlike on the api where the caller is
		// always known from their token: this package never reads claims
		// for identity. Without it the listing simply omits whether each
		// text is followed, since there is nobody to answer that about.
		userID := r.URL.Query().Get("userId")

		results := make([]recentTextResult, 0, len(texts))
		for _, text := range texts {
			roundNumber, err := repo.RoundCount(text.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}

			subscribed := false
			if userID != "" {
				if subscribed, err = repo.IsSubscribed(userID, text.ID); err != nil {
					writeError(w, http.StatusInternalServerError, "internal error")
					return
				}
			}

			results = append(results, recentTextResult{
				Text:        text,
				RoundNumber: roundNumber,
				Subscribed:  subscribed,
			})
		}
		writeJSON(w, http.StatusOK, results)
	}
}

// recentTextResult mirrors the api's own listing shape: a text plus which
// round it is on, and — when a userId was given — whether that user
// follows it.
//
// The text is embedded rather than restated field by field, so this stays
// a decoration of queel.Text instead of a second definition of it that
// could drift.
type recentTextResult struct {
	*queel.Text
	RoundNumber int  `json:"roundNumber"`
	Subscribed  bool `json:"subscribed"`
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
		if !checkSubscription(w, repo, r.PathValue("id"), r.URL.Query().Get("userId")) {
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

// deleteTextHandler removes a text along with its rounds/fragments/votes/
// subscriptions — see queel.Repository.DeleteText. Gated by
// rbac.ActionCreateText, matching api/texts.go's deleteTextHandler: deleting
// is treated as an extension of authoring rights, not its own permission.
func deleteTextHandler(repo *queel.Repository, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAction(w, r, jwtSecret, rbac.ActionCreateText) {
			return
		}
		if !checkSubscription(w, repo, r.PathValue("id"), r.URL.Query().Get("userId")) {
			return
		}

		if err := repo.DeleteText(r.PathValue("id")); err != nil {
			writeRepositoryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type proposeEditRequest struct {
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Content  string `json:"content"`
	AuthorID string `json:"authorId"`
}

// proposeEditHandler requires rbac.ActionEditText, whether the range opens
// a zone nobody had opened or competes on one already open: the two are the
// same act as far as rights go.
func proposeEditHandler(repo *queel.Repository, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		textID := r.PathValue("id")

		var req proposeEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		// One right to edit, whichever zone is aimed at: opening one nobody
		// had opened and competing on one already open are the same act as
		// far as rights go. Where a zone may be opened is settled by one
		// structural rule and no privilege — it must not overlap another
		// (queel.ErrOverlappingSlot).
		if !checkAction(w, r, jwtSecret, rbac.ActionEditText) {
			return
		}

		// This route already names its author, so unlike the three below
		// the rule applies without a query parameter.
		if !checkSubscription(w, repo, textID, req.AuthorID) {
			return
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
		if !checkSubscription(w, repo, r.PathValue("id"), r.URL.Query().Get("userId")) {
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

// scheduleCloseMaxDays bounds how far out a round's close may be scheduled —
// mirrors api/slots.go's maxScheduleCloseDays.
const scheduleCloseMaxDays = 365

type scheduleCloseRequest struct {
	Days int `json:"days"`
}

type scheduleCloseResponse struct {
	ScheduledCloseAt time.Time `json:"scheduledCloseAt"`
}

// scheduleCloseHandler is the "close in N days" alternative to
// closeRoundHandler's immediate close — see
// queel.Repository.ScheduleRoundClose. Mirrors api/slots.go's
// scheduleCloseHandler.
func scheduleCloseHandler(repo *queel.Repository, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAction(w, r, jwtSecret, rbac.ActionCloseText) {
			return
		}
		if !checkSubscription(w, repo, r.PathValue("id"), r.URL.Query().Get("userId")) {
			return
		}

		var req scheduleCloseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Days < 1 || req.Days > scheduleCloseMaxDays {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("days must be between 1 and %d", scheduleCloseMaxDays))
			return
		}

		closeAt := time.Now().Add(time.Duration(req.Days) * 24 * time.Hour)
		// userId names the caller, as everywhere in this package: it never
		// reads a token for identity. Empty simply excludes nobody from
		// the notification the close will eventually send.
		round, err := repo.ScheduleRoundClose(r.PathValue("id"), closeAt, r.URL.Query().Get("userId"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, scheduleCloseResponse{ScheduledCloseAt: *round.ScheduledCloseAt})
	}
}

type subscribeRequest struct {
	UserID string `json:"userId"`
}

type subscribeResponse struct {
	Subscribed bool `json:"subscribed"`
}

// subscribeHandler lets a caller follow a text — see queel.Subscription's
// doc comment: a personal focus signal, not a permission grant, so unlike
// the routes above this isn't gated by any rbac.Action. Mirrors
// api/subscriptions.go's subscribeHandler; since server never derives
// identity from a token (see the package doc), the caller passes userId
// directly instead of it coming from JWT claims.
func subscribeHandler(repo *queel.Repository, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Mirrors the api's own gating (rbac.ActionSubscribe): following a
		// text is what surfaces its vote/edit/close actions and what puts
		// someone on the notification fan-out, so it is a decision an
		// install gets to make per user. Identity still comes from the
		// body, as everywhere in this package — the token is read for
		// permissions only, never for who the caller is.
		if !checkAction(w, r, jwtSecret, rbac.ActionSubscribe) {
			return
		}

		var req subscribeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if _, err := repo.Subscribe(req.UserID, r.PathValue("id")); err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, subscribeResponse{Subscribed: true})
	}
}

// subscribedText mirrors the api's own listing shape: id and title only,
// since this exists to render a list of followed titles and the full
// content of every one of them would be dead weight.
type subscribedText struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// subscriptionsHandler lists the texts a user follows. As everywhere in
// this package, identity is passed in — here as the {userId} path segment —
// rather than read from a token (see the package doc), matching how
// subscribeHandler above takes it in the body.
//
// A subscription whose text no longer exists is skipped: nothing prunes
// subscriptions when a text is deleted, so one dangling id must not fail
// the whole listing.
// userVotesHandler mirrors the api's own my-votes route: which fragment a
// user currently has voted for in each slot of a text, keyed by slot id.
//
// The user is named in the path rather than taken from a token, as
// everywhere in this package — it never reads claims for identity, only
// for permissions. Ungated for the same reason the api leaves it ungated:
// reading choices already made is not voting.
func userVotesHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		votes, err := repo.UserVotes(r.PathValue("id"), r.PathValue("userId"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}

		// Never nil, so an empty result marshals as {} rather than null.
		writeJSON(w, http.StatusOK, votes)
	}
}

// historyVersion mirrors the api's own history shape: one link of a text's
// chain of versions, with the rounds that ran on it.
type historyVersion struct {
	TextID    string         `json:"textId"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Finalized bool           `json:"finalized"`
	Rounds    []historyRound `json:"rounds"`
}

type historyRound struct {
	Number int           `json:"number"`
	Status string        `json:"status"`
	Slots  []historySlot `json:"slots"`
}

type historySlot struct {
	SlotID   string `json:"slotId"`
	Original string `json:"original"`
	Winner   string `json:"winner"`
	Votes    int    `json:"votes"`
	AuthorID string `json:"authorId,omitempty"`
}

// historyHandler returns every version of a text, oldest first, each with
// the rounds that ran on it and how their slots were settled.
//
// Any version's id is a valid entry point — the chain is walked both ways
// (see queel.Repository.TextChain). Ungated, like every other read here.
func historyHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chain, err := repo.TextChain(r.PathValue("id"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}

		versions := make([]historyVersion, 0, len(chain))
		for _, text := range chain {
			rounds, err := repo.RoundsForText(text.ID)
			if err != nil {
				writeRepositoryError(w, err)
				return
			}

			version := historyVersion{
				TextID:    text.ID,
				Title:     text.Title,
				Content:   text.Content,
				Finalized: text.Finalized,
				Rounds:    make([]historyRound, 0, len(rounds)),
			}

			runes := []rune(text.Content)
			for _, round := range rounds {
				resolved := historyRound{
					Number: round.Number,
					Status: string(round.Status),
					Slots:  make([]historySlot, 0, len(round.Slots)),
				}

				for _, slot := range round.Slots {
					winner, err := repo.WinningFragment(text.ID, slot.ID)
					if err != nil {
						if errors.Is(err, queel.ErrNotFound) {
							continue
						}
						writeRepositoryError(w, err)
						return
					}
					votes, err := repo.VoteCount(winner.ID)
					if err != nil {
						writeRepositoryError(w, err)
						return
					}
					if slot.Start < 0 || slot.End > len(runes) || slot.Start > slot.End {
						continue
					}

					resolved.Slots = append(resolved.Slots, historySlot{
						SlotID:   slot.ID,
						Original: string(runes[slot.Start:slot.End]),
						Winner:   winner.Content,
						Votes:    votes,
						AuthorID: winner.AuthorID,
					})
				}

				version.Rounds = append(version.Rounds, resolved)
			}

			versions = append(versions, version)
		}

		writeJSON(w, http.StatusOK, versions)
	}
}

func subscriptionsHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		textIDs, err := repo.SubscriptionsForUser(r.PathValue("userId"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}

		// Never nil, so an empty list marshals as [] rather than null.
		texts := make([]subscribedText, 0, len(textIDs))
		for _, id := range textIDs {
			text, err := repo.Text(id)
			if err != nil {
				if errors.Is(err, queel.ErrNotFound) {
					continue
				}
				writeRepositoryError(w, err)
				return
			}
			texts = append(texts, subscribedText{ID: text.ID, Title: text.Title})
		}

		writeJSON(w, http.StatusOK, texts)
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
