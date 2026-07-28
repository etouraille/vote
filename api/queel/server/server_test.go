package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/etouraille/queel"
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
