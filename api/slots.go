package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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

// searchTextsHandler answers RAG search over the finalized-text corpus: the
// query is embedded the same way the indexed texts were, and Qdrant returns
// the closest matches by cosine similarity. Each result's round number is
// filled in live from queel (not stored in the vector index, which would go
// stale the moment a round opens or closes after indexing) — via
// RoundCount, not CurrentRound: a text whose round has already closed still
// reports the round that produced it, rather than looking indistinguishable
// from one that never had a round at all. 0 means no round has ever been
// opened on that text.
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

		for i, result := range results {
			count, err := repo.RoundCount(result.TextID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "erreur serveur")
				return
			}
			results[i].RoundNumber = count
		}

		writeJSON(w, http.StatusOK, results)
	}
}
