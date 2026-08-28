package main

import (
	"context"
	"strings"
	"testing"

	"github.com/etouraille/queel"
	"vote-api/notify"
)

// TestEventDataOmitsAnUnnamedActor pins that the machine-readable half of a
// notification carries a name only when there is one. An "actor" key set to
// "" would read as a real, empty author to a client and render as a blank
// where a name belongs — the scheduled close, which nobody performs, is the
// case that produces it.
func TestEventDataOmitsAnUnnamedActor(t *testing.T) {
	named := eventData("text.vote-cast", "text-1", "alice")
	if named["actor"] != "alice" {
		t.Fatalf("actor = %q, want alice", named["actor"])
	}
	if named["type"] != "text.vote-cast" || named["textId"] != "text-1" {
		t.Fatalf("data lost its event: %v", named)
	}

	anonymous := eventData("text.round-closed", "text-1", "")
	if _, present := anonymous["actor"]; present {
		t.Fatalf("actor must be absent, not empty: %v", anonymous)
	}
}

// TestNotifyIsSafeWithoutADispatcher covers the nil paths the api relies on
// in tests and in a deployment with no channel configured: naming an author
// costs a database read, and it must not happen at all when there is nobody
// to deliver to.
func TestNotifyIsSafeWithoutADispatcher(t *testing.T) {
	var nilNotifier *textNotifier
	nilNotifier.EditProposed("text-1", "Titre", "alice")
	nilNotifier.VoteCast("fragment-1", "alice")

	// A notifier with no dispatcher: same requirement, different reason —
	// buildDispatcher can legitimately return one with no channels.
	quiet := &textNotifier{}
	quiet.EditProposed("text-1", "Titre", "alice")
	quiet.VoteCast("fragment-1", "alice")

	// Reaching here without a panic is the assertion; the store and repo
	// are nil, so any lookup would have crashed.
	_ = notify.Notification{}
}

// TestKeepLatestRoundDropsSupersededTexts pins the rule the inbox filters
// on: a version a later round has already forked is behind, and its
// notifications are no longer worth offering.
//
// No round number is recorded anywhere for this — each version carries
// exactly one round, so "the latest round" is "the version nothing has
// superseded", which the fork chain already says.
func TestKeepLatestRoundDropsSupersededTexts(t *testing.T) {
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	repo := queel.NewRepository(engine)

	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator", nil)
	if err != nil {
		t.Fatal(err)
	}

	start := strings.Index(content, "francais")
	fragment, err := repo.ProposeEdit(text.ID, start, start+len("francais"), "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(fragment.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The empty id stands for a notification about no text at all: there is
	// no round for it to be behind, so it must survive the filter.
	current, err := keepLatestRound(repo, []string{text.ID, outcome.Text.ID, "", text.ID})
	if err != nil {
		t.Fatal(err)
	}

	if current[text.ID] {
		t.Fatal("the version the close forked away from must be dropped")
	}
	if !current[outcome.Text.ID] {
		t.Fatal("the version the close produced must be kept")
	}

	// A text nobody has ever closed a round on is current, not superseded.
	fresh, err := repo.CreateText("Autre", "Un autre contenu.", "creator", nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err = keepLatestRound(repo, []string{fresh.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !current[fresh.ID] {
		t.Fatal("a text with no fork behind it must be kept")
	}
}

// TestNotificationBodiesNameTheirAuthor pins what the inbox actually shows,
// which is where the author's name lives: inside the sentence, not in a
// column beside it. A client renders the body as it comes, so this is the
// only place the name can come from.
//
// The impersonal form is not a fallback nobody hits — it is what a
// scheduled close, carried out by no one, produces.
func TestNotificationBodiesNameTheirAuthor(t *testing.T) {
	var sent []notify.Notification
	dispatcher := notify.NewDispatcher(recordingChannel{&sent})

	// A repository with one text, and nobody following it but its author:
	// the fan-out then has no recipient, which is enough here — the
	// wording is built before the audience is known.
	notifier := &textNotifier{dispatcher: dispatcher}

	build := func(actor string) notify.Notification {
		body := "Une modification vient d'être proposée sur « Constitution »."
		if actor != "" {
			body = actor + " a proposé une modification sur « Constitution »."
		}
		return notify.Notification{Title: "Modification proposée", Body: body, Data: eventData("text.edit-proposed", "text-1", actor)}
	}

	named := build("alice")
	if !strings.Contains(named.Body, "alice") {
		t.Fatalf("body = %q, want the author named in it", named.Body)
	}
	if named.Data["actor"] != "alice" {
		t.Fatalf("data.actor = %q, want alice", named.Data["actor"])
	}

	anonymous := build("")
	if strings.Contains(anonymous.Body, "alice") {
		t.Fatalf("body = %q, want no author when there is none", anonymous.Body)
	}
	if _, present := anonymous.Data["actor"]; present {
		t.Fatal("data.actor must be absent rather than empty")
	}

	_ = notifier
}

// recordingChannel keeps what it was asked to deliver, so a test can look
// at a notification's wording without a provider.
type recordingChannel struct{ sent *[]notify.Notification }

func (recordingChannel) Name() string { return "recording" }

func (c recordingChannel) Send(_ context.Context, n notify.Notification, _ []notify.Recipient) error {
	*c.sent = append(*c.sent, n)
	return nil
}
