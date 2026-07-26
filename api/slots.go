package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/rbac"
)

type proposeEditRequest struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Content string `json:"content"`
}

// textSupersededResponse is what proposeEditHandler answers with when
// queel.ErrTextSuperseded fires: textID has already been forked by a closed
// round, so supersededBy names the current version to retry against instead
// — enough for the front end to redirect there on its own rather than just
// showing a dead end.
type textSupersededResponse struct {
	Error        string `json:"error"`
	SupersededBy string `json:"supersededBy"`
}

// proposeEditHandler opens (or joins) a voting slot on a text: the
// [start, end) rune range being replaced, and the author's proposed content
// for it. Competing proposals for the same range accumulate here until the
// round is closed and voted fragments are decided.
//
// Which permission this requires depends on whether the range is brand new
// or already an open slot in the current round (see rbac.ActionSelect vs
// rbac.ActionEditSelection) — carving out a new range is the more
// consequential half of the operation, so it's gated separately from just
// proposing content for a range someone already selected.
func proposeEditHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		textID := r.PathValue("id")

		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}

		var req proposeEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "corps de requête trop volumineux")
				return
			}
			writeError(w, http.StatusBadRequest, "corps de requête invalide")
			return
		}

		if req.Start < 0 || req.End <= req.Start {
			writeError(w, http.StatusBadRequest, "plage de sélection invalide")
			return
		}
		if utf8.RuneCountInString(req.Content) > maxContentRunes {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("le contenu ne doit pas dépasser %d caractères", maxContentRunes))
			return
		}

		action := rbac.ActionSelect
		if round, err := repo.CurrentRound(textID); err == nil {
			for _, slot := range round.Slots {
				if slot.Start == req.Start && slot.End == req.End {
					action = rbac.ActionEditSelection
					break
				}
			}
		} else if !errors.Is(err, queel.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}
		if !claims.Allows(action) {
			writeError(w, http.StatusForbidden, "droits insuffisants")
			return
		}

		fragment, err := repo.ProposeEdit(textID, req.Start, req.End, req.Content, claims.Subject)
		if err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "texte introuvable")
				return
			}
			var superseded *queel.ErrTextSuperseded
			if errors.As(err, &superseded) {
				writeJSON(w, http.StatusConflict, textSupersededResponse{
					Error:        "ce texte a déjà été remplacé par une version plus récente",
					SupersededBy: superseded.SupersededBy,
				})
				return
			}
			// The only other errors ProposeEdit returns are client-caused
			// (invalid range, overlapping slot) and safe to relay as-is.
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, fragment)
	}
}

// closeRoundHandler finalizes the current voting round: winning fragments
// are spliced into a brand new, Finalized text forked from the one the round
// was open on (outcome.Text.ID is a new ID, see queel.Text.PreviousTextID —
// the original text and round are left untouched), and (best-effort) the new
// text is indexed into the RAG search corpus. Indexing failures don't undo
// the round closure — search is an enhancement on top of the voting
// workflow, not a dependency of it.
func closeRoundHandler(repo *queel.Repository, index *searchIndexer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePermission(w, r, rbac.ActionCloseText) {
			return
		}

		textID := r.PathValue("id")

		outcome, err := repo.CloseRound(textID)
		if err != nil {
			if errors.Is(err, queel.ErrNoOpenRound) {
				writeError(w, http.StatusConflict, "aucun tour de vote ouvert pour ce texte")
				return
			}
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "texte introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		if err := index.IndexFinalizedText(r.Context(), outcome.Text); err != nil {
			log.Printf("failed to index text %s (forked from %s) after closing round: %v", outcome.Text.ID, textID, err)
		}

		writeJSON(w, http.StatusOK, outcome)
	}
}

// maxScheduleCloseDays bounds how far out a round's close can be scheduled —
// generous enough for any real deliberation window while keeping a runaway
// or malicious value from parking a round open indefinitely.
const maxScheduleCloseDays = 365

type scheduleCloseRequest struct {
	Days int `json:"days"`
}

type scheduleCloseResponse struct {
	ScheduledCloseAt time.Time `json:"scheduledCloseAt"`
}

// scheduleCloseHandler is the "close in N days" alternative to
// closeRoundHandler's immediate close: it only records a due date on the
// current round (see queel.Repository.ScheduleRoundClose) — the round stays
// open, still accepting proposals and votes, until runScheduledCloseWorker
// (see main.go) actually calls CloseRound on it once that date arrives.
// Gated by the same permission as closing outright, since scheduling one is
// just as consequential — it can't be walked back once the worker picks it
// up.
func scheduleCloseHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePermission(w, r, rbac.ActionCloseText) {
			return
		}

		textID := r.PathValue("id")

		var req scheduleCloseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "corps de requête trop volumineux")
				return
			}
			writeError(w, http.StatusBadRequest, "corps de requête invalide")
			return
		}
		if req.Days < 1 || req.Days > maxScheduleCloseDays {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("le nombre de jours doit être compris entre 1 et %d", maxScheduleCloseDays))
			return
		}

		closeAt := time.Now().Add(time.Duration(req.Days) * 24 * time.Hour)
		round, err := repo.ScheduleRoundClose(textID, closeAt)
		if err != nil {
			if errors.Is(err, queel.ErrNoOpenRound) {
				writeError(w, http.StatusConflict, "aucun tour de vote ouvert pour ce texte")
				return
			}
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "texte introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		writeJSON(w, http.StatusOK, scheduleCloseResponse{ScheduledCloseAt: *round.ScheduledCloseAt})
	}
}

// searchTextsHandler answers RAG search over the finalized-text corpus: the
// query is embedded the same way the indexed texts were, and Qdrant returns
// the closest matches by cosine similarity. Each result's round number is
// filled in live from queel (not stored in the vector index, which would go
// stale the moment a round opens or closes after indexing) — via
// RoundCount, not CurrentRound: a text whose round has already closed still
// reports the round that produced it, rather than looking indistinguishable
// from one that never had a round at all. 0 means no round has ever been
// opened on that text.
//
// Results already superseded by a newer fork (queel.Repository.IsSuperseded
// — i.e. a version of this text with a strictly higher round count exists
// elsewhere in its chain) are dropped rather than returned: without
// SEARCH_PRUNE_SUPERSEDED, every closed round leaves its pre-round version
// indexed too (see IndexFinalizedText's doc comment), and even with it on,
// that removal is best-effort at index time. Filtering here instead means
// the search bar only ever shows the current head of each version chain
// regardless of that setting or whether a prune attempt happened to fail.
func searchTextsHandler(index *searchIndexer, repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if len(q) == 0 {
			writeError(w, http.StatusBadRequest, "le paramètre de recherche q est obligatoire")
			return
		}

		results, err := index.Search(r.Context(), q, 10)
		if err != nil {
			log.Printf("search failed: %v", err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		current := make([]SearchResult, 0, len(results))
		for _, result := range results {
			superseded, err := repo.IsSuperseded(result.TextID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "erreur serveur")
				return
			}
			if superseded {
				continue
			}

			count, err := repo.RoundCount(result.TextID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "erreur serveur")
				return
			}
			result.RoundNumber = count
			current = append(current, result)
		}

		writeJSON(w, http.StatusOK, current)
	}
}
