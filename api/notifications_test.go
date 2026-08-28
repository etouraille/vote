package main

import (
	"testing"

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
