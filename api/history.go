package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/etouraille/queel"
)

// historySlot is how one slot of a past round is reported: the wording it
// replaced, the wording that won, and by how many votes.
//
// Original is sliced out of the version the round was open on, not the one
// it produced — that is the whole point of showing it, and it is the only
// place that wording still exists once the fork has spliced the winner in.
type historySlot struct {
	SlotID   string `json:"slotId"`
	Original string `json:"original"`
	Winner   string `json:"winner"`
	Votes    int    `json:"votes"`
	AuthorID string `json:"authorId,omitempty"`
}

// historyRound is one round of one version, with what it settled.
type historyRound struct {
	Number int           `json:"number"`
	Status string        `json:"status"`
	Slots  []historySlot `json:"slots"`
}

// historyVersion is one link of the chain: a version of the text and the
// round that ran on it.
type historyVersion struct {
	TextID    string         `json:"textId"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	CreatedAt string         `json:"createdAt"`
	Finalized bool           `json:"finalized"`
	Rounds    []historyRound `json:"rounds"`
}

// historyHandler returns the whole life of a text: every version from the
// original to the current one, and for each the round that ran on it with
// its slots resolved.
//
// It exists because none of this was reachable. Closing a round has always
// preserved everything — the round record is rewritten rather than deleted
// (see queel's CloseRound), the superseded version is left untouched, and
// its fragments and votes stay put — but no route ever read any of it back,
// so a text's past was stored and invisible at the same time.
//
// Any version's id is a valid entry point: the chain is walked in both
// directions (see queel's TextChain), so a link found in an old
// notification leads to the same history as the current version does.
func historyHandler(repo *queel.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chain, err := repo.TextChain(r.PathValue("id"))
		if err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				writeError(w, http.StatusNotFound, "texte introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		versions := make([]historyVersion, 0, len(chain))
		for _, text := range chain {
			rounds, err := repo.RoundsForText(text.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "erreur serveur")
				return
			}

			version := historyVersion{
				TextID:    text.ID,
				Title:     text.Title,
				Content:   text.Content,
				CreatedAt: text.CreatedAt.Format(time.RFC3339),
				Finalized: text.Finalized,
				Rounds:    make([]historyRound, 0, len(rounds)),
			}

			for _, round := range rounds {
				resolved, err := resolveHistoryRound(repo, text, round)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "erreur serveur")
					return
				}
				version.Rounds = append(version.Rounds, resolved)
			}

			versions = append(versions, version)
		}

		writeJSON(w, http.StatusOK, versions)
	}
}

// resolveHistoryRound fills in each slot's outcome by recomputing it from
// the fragments and votes still in the store, rather than reading a stored
// result — queel keeps no RoundOutcome, so this is the only source, and it
// is also what keeps a *still open* round reportable: its "winner" is
// simply whoever leads right now.
func resolveHistoryRound(repo *queel.Repository, text *queel.Text, round *queel.Round) (historyRound, error) {
	resolved := historyRound{
		Number: round.Number,
		Status: string(round.Status),
		// Never nil: an open round with nothing proposed has to marshal as
		// [] rather than null.
		Slots: make([]historySlot, 0, len(round.Slots)),
	}

	runes := []rune(text.Content)
	for _, slot := range round.Slots {
		winner, err := repo.WinningFragment(text.ID, slot.ID)
		if err != nil {
			if errors.Is(err, queel.ErrNotFound) {
				continue
			}
			return historyRound{}, err
		}
		votes, err := repo.VoteCount(winner.ID)
		if err != nil {
			return historyRound{}, err
		}

		// Slot bounds are rune offsets into the version the round opened
		// on. A slot pointing outside it would mean the two disagree;
		// skipped rather than panicking on the slice.
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

	return resolved, nil
}
