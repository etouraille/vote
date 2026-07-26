package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/rbac"
)

// Length caps on top of the global request body size limit (withBodyLimit):
// even within a 2 MiB request, a single absurdly long title or content string
// is rejected outright rather than accepted and handed to queel/storage.
const (
	maxTitleRunes   = 300
	maxContentRunes = 300_000
)

// defaultRecentTextsLimit and maxRecentTextsLimit bound the ?limit= query
// param on GET /api/texts — a sane default when it's omitted, and a ceiling
// so a caller can't force an unbounded full-corpus scan.
const (
	defaultRecentTextsLimit = 20
	maxRecentTextsLimit     = 100
)

type textPayload struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type textResponse struct {
	ID string `json:"id"`
}

// decodeTextPayload reads and validates a {title, content} body shared by
// create and update, writing the appropriate error response itself when
// invalid. ok reports whether the caller should proceed.
func decodeTextPayload(w http.ResponseWriter, r *http.Request) (title, content string, ok bool) {
	var req textPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "corps de requête trop volumineux")
			return "", "", false
		}
		writeError(w, http.StatusBadRequest, "corps de requête invalide")
		return "", "", false
	}

	title = strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "le titre est obligatoire")
		return "", "", false
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("le titre ne doit pas dépasser %d caractères", maxTitleRunes))
		return "", "", false
	}
	if utf8.RuneCountInString(req.Content) > maxContentRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("le contenu ne doit pas dépasser %d caractères", maxContentRunes))
		return "", "", false
	}

	return title, req.Content, true
}

// createTextHandler creates a text and (best-effort) indexes it into the
// search corpus right away — unlike closeRoundHandler's indexing, which
// only ever sees a text once it's finalized, this means a brand-new draft
// is searchable immediately, not just after its first round closes.
// Indexing failures don't fail the request: search is an enhancement on
// top of text creation, not a dependency of it.
func createTextHandler(repo *queel.Repository, index *searchIndexer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok || !claims.Allows(rbac.ActionCreateText) {
			writeError(w, http.StatusForbidden, "droits insuffisants")
			return
		}

		title, content, ok := decodeTextPayload(w, r)
		if !ok {
			return
		}

		text, err := repo.CreateText(title, content, claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		if err := index.IndexText(r.Context(), text); err != nil {
			log.Printf("failed to index text %s after creation: %v", text.ID, err)
		}

		writeJSON(w, http.StatusCreated, textResponse{ID: text.ID})
	}
}

// recentTextsHandler lists the most recently created texts — for the home
// page's "latest texts" cards, e.g.
func recentTextsHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := defaultRecentTextsLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeError(w, http.StatusBadRequest, "limit invalide")
				return
			}
			limit = parsed
		}
		if limit > maxRecentTextsLimit {
			limit = maxRecentTextsLimit
		}

		texts, err := repo.RecentTexts(limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}
		writeJSON(w, http.StatusOK, texts)
	}
}

// getTextHandler fetches a single text by ID, for opening an existing text
// (from a search result, e.g.) in the editor.
func getTextHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		text, err := repo.Text(id)
		if err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "texte introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		writeJSON(w, http.StatusOK, text)
	}
}

// textWithSlotsHandler joins a text with the slots of its current round —
// see queel.Repository.TextWithSlots. No round open isn't an error: it's a
// 200 with roundNumber 0 and an empty slots list, not a 404.
func textWithSlotsHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := repo.TextWithSlots(r.PathValue("id"))
		if err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "texte introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// fragmentsForSlotHandler lists every candidate fragment competing for a
// slot — a read, so no permission gate, same as getTextHandler.
func fragmentsForSlotHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fragments, err := repo.Fragments(r.PathValue("id"), r.PathValue("slotId"))
		if err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "texte introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}
		writeJSON(w, http.StatusOK, fragments)
	}
}

// updateTextHandler overwrites an existing text's title/content directly,
// addressed only by its ID in the URL — entirely separate from the
// round/slot/vote workflow, so it works the same whether or not a voting
// round happens to be open on that text.
func updateTextHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePermission(w, r, rbac.ActionUpdateText) {
			return
		}

		id := r.PathValue("id")

		title, content, ok := decodeTextPayload(w, r)
		if !ok {
			return
		}

		text, err := repo.UpdateText(id, title, content)
		if err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "texte introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		writeJSON(w, http.StatusOK, textResponse{ID: text.ID})
	}
}

// deleteTextHandler removes a single text outright — see
// queel.Repository.DeleteText for exactly what that cascades to (its
// rounds/fragments/votes, but not any text it was later forked into).
// Gated on the same right as creating a text in the first place
// (rbac.ActionCreateText) rather than a dedicated permission bit — whoever
// can add a text to the corpus can also remove one.
func deleteTextHandler(repo *queel.Repository, index *searchIndexer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok || !claims.Allows(rbac.ActionCreateText) {
			writeError(w, http.StatusForbidden, "droits insuffisants")
			return
		}

		id := r.PathValue("id")

		deleteErr := repo.DeleteText(id)
		if deleteErr != nil && !errors.Is(deleteErr, queel.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		// Best-effort even when queel already had no such text — a search
		// index entry can outlive it (e.g. left behind by a deletion path
		// that predates this one, DeleteUserTexts, which never purged the
		// index). Nothing should keep serving a result for a text that's
		// gone just because there's nothing left on the queel side to
		// "delete" a second time.
		if err := index.RemoveText(r.Context(), id); err != nil {
			log.Printf("failed to remove text %s from the search index after deletion: %v", id, err)
		}

		if errors.Is(deleteErr, queel.ErrNotFound) {
			writeError(w, http.StatusNotFound, "texte introuvable")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
