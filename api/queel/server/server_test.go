package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/rbac"
)

func newTestRepo(t *testing.T) *queel.Repository {
	t.Helper()
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return queel.NewRepository(engine)
}

func TestDeleteTextHandlerRemovesText(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	text, err := repo.CreateText("Titre", "Contenu", "author-1")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/texts/"+text.ID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := repo.Text(text.ID); !errors.Is(err, queel.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got err=%v", err)
	}
}

func TestDeleteTextHandlerUnknownText(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest(http.MethodDelete, "/texts/does-not-exist", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestScheduleCloseHandlerSchedulesTheOpenRound(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	text, err := repo.CreateText("Titre", "Contenu du texte", "author-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(text.ID, 0, 7, "Modifié", "author-1"); err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"days":3}`)
	req := httptest.NewRequest(http.MethodPost, "/texts/"+text.ID+"/schedule-close", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp scheduleCloseResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ScheduledCloseAt.IsZero() {
		t.Fatal("expected a non-zero scheduledCloseAt")
	}

	round, err := repo.CurrentRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if round.ScheduledCloseAt == nil {
		t.Fatal("expected the round to carry a ScheduledCloseAt")
	}
}

func TestScheduleCloseHandlerRejectsOutOfRangeDays(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	text, err := repo.CreateText("Titre", "Contenu du texte", "author-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ProposeEdit(text.ID, 0, 7, "Modifié", "author-1"); err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"days":0}`)
	req := httptest.NewRequest(http.MethodPost, "/texts/"+text.ID+"/schedule-close", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubscribeHandlerSubscribesTheGivenUser(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	text, err := repo.CreateText("Titre", "Contenu", "author-1")
	if err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"userId":"reader-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/texts/"+text.ID+"/subscribe", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	subscribed, err := repo.IsSubscribed("reader-1", text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !subscribed {
		t.Fatal("expected reader-1 to be subscribed")
	}
}

func TestSubscribeHandlerUnknownText(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	body := strings.NewReader(`{"userId":"reader-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/texts/does-not-exist/subscribe", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubscriptionsHandlerListsFollowedTitles(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	first, err := repo.CreateText("Premier", "Contenu", "author-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateText("Second", "Contenu", "author-1")
	if err != nil {
		t.Fatal(err)
	}
	// A third text nobody follows, to prove the listing is per-user and
	// not just "every text there is".
	if _, err := repo.CreateText("Ignoré", "Contenu", "author-1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first.ID, second.ID} {
		if _, err := repo.Subscribe("reader-1", id); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/users/reader-1/subscriptions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []subscribedText
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d: %s", len(got), rec.Body.String())
	}
	titles := map[string]bool{got[0].Title: true, got[1].Title: true}
	if !titles["Premier"] || !titles["Second"] {
		t.Fatalf("expected the two followed titles, got %s", rec.Body.String())
	}
}

// [] rather than null: clients iterate the response without special-casing
// "no subscriptions yet".
func TestSubscriptionsHandlerReturnsEmptyArrayForNoSubscriptions(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/users/nobody/subscriptions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Fatalf("expected [], got %q", body)
	}
}

// Nothing prunes subscriptions when a text is deleted, so a dangling id
// must be skipped rather than failing every other subscription.
func TestSubscriptionsHandlerSkipsDeletedTexts(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	kept, err := repo.CreateText("Toujours là", "Contenu", "author-1")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := repo.CreateText("Supprimé", "Contenu", "author-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{kept.ID, removed.ID} {
		if _, err := repo.Subscribe("reader-1", id); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.DeleteText(removed.ID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users/reader-1/subscriptions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []subscribedText
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Toujours là" {
		t.Fatalf("expected only the surviving text, got %s", rec.Body.String())
	}
}

// testSecret is any non-empty secret: checkAction disables authorization
// entirely when the secret is empty (which is why every other test here
// passes nil), so exercising the gate at all requires one.
var testSecret = []byte("test-secret")

func subscribeTokenFor(t *testing.T, perms rbac.Permissions) string {
	t.Helper()
	token, err := rbac.SignToken(rbac.Claims{
		Subject:   "reader-1",
		Perms:     perms.Bits(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// subscribeRequestFor builds the same request the three cases below differ
// only in the token of.
func subscribeRequestFor(textID, token string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/texts/"+textID+"/subscribe", strings.NewReader(`{"userId":"reader-1"}`))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestSubscribeHandlerRequiresSubscribePermission(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, testSecret)

	text, err := repo.CreateText("Titre", "Contenu", "author-1")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"no token", "", http.StatusUnauthorized},
		// CanVote alone is the telling case: it proves the gate reads the
		// subscribe bit specifically, not merely "has some permission".
		{"without the bit", subscribeTokenFor(t, rbac.Permissions{CanVote: true}), http.StatusForbidden},
		{"with the bit", subscribeTokenFor(t, rbac.Permissions{CanSubscribe: true}), http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, subscribeRequestFor(text.ID, tc.token))

			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}

	// The granted case must actually have subscribed, not just answered 200.
	subscribed, err := repo.IsSubscribed("reader-1", text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !subscribed {
		t.Fatal("expected reader-1 to be subscribed")
	}
}

func TestUserVotesHandlerReportsEachSlotsCurrentChoice(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "author-1")
	if err != nil {
		t.Fatal(err)
	}

	start := strings.Index(content, "francais")
	fragment, err := repo.ProposeEdit(text.ID, start, start+len("francais"), "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(fragment.ID, "reader-1"); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/texts/"+text.ID+"/votes/reader-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got[fragment.SlotID] != fragment.ID {
		t.Fatalf("vote for slot %s = %q, want %q", fragment.SlotID, got[fragment.SlotID], fragment.ID)
	}

	// Scoped to the user asked about: reader-2 voted for nothing, and must
	// not inherit reader-1's choice from the same slot.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/texts/"+text.ID+"/votes/reader-2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "{}" {
		t.Fatalf("a user who never voted = %s, want {}", body)
	}
}

// TestHistoryHandlerWalksTheWholeChain closes a round and then asks for the
// history from *both* ends of the resulting chain: the point of the route
// is that an old id and the current one lead to the same story.
func TestHistoryHandlerWalksTheWholeChain(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	content := "Nous le peuple francais declare."
	original, err := repo.CreateText("Constitution", content, "author-1")
	if err != nil {
		t.Fatal(err)
	}

	start := strings.Index(content, "francais")
	fragment, err := repo.ProposeEdit(original.ID, start, start+len("francais"), "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(fragment.ID, "reader-1"); err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.CloseRound(original.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, entryPoint := range []struct {
		name string
		id   string
	}{
		{"from the original", original.ID},
		{"from the fork", outcome.Text.ID},
	} {
		t.Run(entryPoint.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/texts/"+entryPoint.id+"/history", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}

			var got []historyVersion
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 {
				t.Fatalf("chain length = %d, want 2 (the original and its fork)", len(got))
			}

			// Oldest first, whichever end was asked from.
			if got[0].TextID != original.ID || got[1].TextID != outcome.Text.ID {
				t.Fatalf("chain = [%s %s], want [%s %s]", got[0].TextID, got[1].TextID, original.ID, outcome.Text.ID)
			}

			// The closed round reports what it settled, against the wording
			// it replaced — which only survives on the original version.
			if len(got[0].Rounds) != 1 {
				t.Fatalf("the original's rounds = %d, want 1", len(got[0].Rounds))
			}
			round := got[0].Rounds[0]
			if round.Status != "closed" || len(round.Slots) != 1 {
				t.Fatalf("round = %+v, want one closed round with one slot", round)
			}
			if round.Slots[0].Original != "francais" || round.Slots[0].Winner != "français" {
				t.Fatalf("slot = %+v, want francais -> français", round.Slots[0])
			}
			if round.Slots[0].Votes != 1 {
				t.Fatalf("Votes = %d, want 1", round.Slots[0].Votes)
			}

			// The fork carries the round that opened with it: still open,
			// nothing proposed yet.
			if len(got[1].Rounds) != 1 || got[1].Rounds[0].Status != "open" {
				t.Fatalf("the fork's rounds = %+v, want a single open one", got[1].Rounds)
			}
		})
	}
}

// TestRecentTextsHandlerDecoratesEachText covers the two halves of the
// listing's shape: the round number, which needs nobody's identity, and
// the follow flag, which needs a userId this package can only be given
// explicitly.
func TestRecentTextsHandlerDecoratesEachText(t *testing.T) {
	repo := newTestRepo(t)
	handler := NewHandler(repo, nil)

	text, err := repo.CreateText("Constitution", "Nous le peuple.", "author-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Subscribe("reader-1", text.ID); err != nil {
		t.Fatal(err)
	}

	get := func(t *testing.T, url string) []recentTextResult {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var got []recentTextResult
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("results = %d, want 1", len(got))
		}
		return got
	}

	// Without a userId: the round is still reported, the follow flag can
	// only be false since nobody was named.
	anonymous := get(t, "/texts")
	if anonymous[0].RoundNumber != 1 {
		t.Fatalf("RoundNumber = %d, want 1 — creation opens the first round", anonymous[0].RoundNumber)
	}
	if anonymous[0].Subscribed {
		t.Fatal("Subscribed must be false when no userId was given")
	}

	// With one, and with one who doesn't follow it.
	if follower := get(t, "/texts?userId=reader-1"); !follower[0].Subscribed {
		t.Fatal("expected reader-1 to be reported as following the text")
	}
	if stranger := get(t, "/texts?userId=reader-2"); stranger[0].Subscribed {
		t.Fatal("expected reader-2 not to be reported as following the text")
	}
}
