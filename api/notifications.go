package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/etouraille/queel"
	"vote-api/notify"
)

// notifyTimeout bounds a fan-out. It runs detached from the request that
// triggered it (see textNotifier.TextUpdated), so nothing else would ever
// stop it.
const notifyTimeout = 30 * time.Second

// textNotifier turns "this text changed" into "these people should hear
// about it", and hands the result to the channels.
//
// It sits between queel (who follows what) and the api's own Postgres
// store (how to reach them) precisely because neither knows about the
// other: subscriptions live in queel, email addresses and device tokens
// live in Postgres.
type textNotifier struct {
	repo       *queel.Repository
	store      *Store
	dispatcher *notify.Dispatcher
}

func newTextNotifier(repo *queel.Repository, store *Store, dispatcher *notify.Dispatcher) *textNotifier {
	return &textNotifier{repo: repo, store: store, dispatcher: dispatcher}
}

// TextUpdated notifies the followers of a text rewritten outright, through
// PUT /api/texts/{id}.
//
// Rarely the one that fires in practice: the Angular editor never calls
// that route — it proposes edits, see EditProposed below. Kept because the
// route exists and a client that does use it should notify all the same.
func (n *textNotifier) TextUpdated(text *queel.Text, actorID string) {
	n.notify(text.ID, actorID, notify.Notification{
		Title: "Texte modifié",
		Body:  fmt.Sprintf("« %s » vient d'être modifié.", text.Title),
		Data: map[string]string{
			"type":   "text.updated",
			"textId": text.ID,
		},
	})
}

// EditProposed notifies the followers of a text somebody has proposed a
// change to — what "modifying a text" actually means in this app: carving
// out a slot and submitting a competing wording for it, which is what the
// editor does and what followers are waiting to vote on.
func (n *textNotifier) EditProposed(textID, title, actorID string) {
	n.notify(textID, actorID, notify.Notification{
		Title: "Modification proposée",
		Body:  fmt.Sprintf("Une modification vient d'être proposée sur « %s ».", title),
		Data: map[string]string{
			"type":   "text.edit-proposed",
			"textId": textID,
		},
	})
}

// RoundClosed notifies the followers of a text whose voting round has just
// been closed — the moment the text actually changes, its winning fragments
// spliced into a new version.
//
// text is the *fork* CloseRound produced, not the text the round was open
// on, and that matters twice over. Closing migrates every subscription to
// the fork and tombstones the originals (see Repository.CloseRound), so
// resolving followers on the old id would find nobody at all. And the fork
// is the version worth reading, so it is the id a tapped notification
// should open.
//
// actorID is empty when the scheduled-close worker is the one closing:
// nobody performed the action, so nobody is excluded from hearing about it.
func (n *textNotifier) RoundClosed(text *queel.Text, actorID string) {
	n.notify(text.ID, actorID, notify.Notification{
		Title: "Tour de vote clos",
		Body:  fmt.Sprintf("Le vote sur « %s » est clos, la nouvelle version est disponible.", text.Title),
		Data: map[string]string{
			// Not edit-proposed: there is no round open on a freshly forked
			// text, so a client following this lands on the text itself
			// rather than on an empty vote page.
			"type":   "text.round-closed",
			"textId": text.ID,
		},
	})
}

// notify delivers n to everyone following textID, except actorID — whoever
// caused the change does not need telling, and since CreateText subscribes
// an author to their own text, skipping this would notify them of every
// edit they make themselves.
//
// Returns immediately: delivery runs in the background on its own context,
// so a slow provider never delays the response to the action that triggered
// it, and cancelling that request doesn't cancel the notification.
func (n *textNotifier) notify(textID, actorID string, notification notify.Notification) {
	if n == nil || n.dispatcher == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		n.deliver(ctx, textID, actorID, notification)
	}()
}

// deliver is notify's body, split out for VoteCast — which has to work out
// which text it is even about before it can name an audience, and so needs
// the fan-out without the goroutine notify wraps around it.
func (n *textNotifier) deliver(ctx context.Context, textID, actorID string, notification notify.Notification) {
	recipients, err := n.recipients(ctx, textID, actorID)
	if err != nil {
		log.Printf("notify: resolving recipients for text %s: %v", textID, err)
		return
	}

	n.dispatcher.Notify(ctx, notification, recipients)
}

// VoteCast notifies the followers of a text somebody has just voted on.
//
// Identified by fragment rather than by text, because that is all the vote
// route knows: a fragment carries the text it belongs to, so the audience
// is resolved from it here. Both lookups run inside the goroutine, off the
// request that cast the vote — the voter waits for their vote to be
// recorded, not for everyone else to be told about it.
//
// Unlike the other events, this one can fire often: a text under active
// voting notifies every follower on every vote.
func (n *textNotifier) VoteCast(fragmentID, actorID string) {
	if n == nil || n.dispatcher == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()

		fragment, err := n.repo.Fragment(fragmentID)
		if err != nil {
			log.Printf("notify: resolving the text of fragment %s: %v", fragmentID, err)
			return
		}
		text, err := n.repo.Text(fragment.TextID)
		if err != nil {
			log.Printf("notify: loading text %s for a vote notification: %v", fragment.TextID, err)
			return
		}

		n.deliver(ctx, text.ID, actorID, notify.Notification{
			Title: "Nouveau vote",
			Body:  fmt.Sprintf("Un vote vient d'être déposé sur « %s ».", text.Title),
			Data: map[string]string{
				// A vote can only be cast while a round is open, so the
				// round is where a tapped notification should land.
				"type":   "text.vote-cast",
				"textId": text.ID,
			},
		})
	}()
}

// recipients resolves the followers of textID into addressable recipients,
// dropping actorID.
func (n *textNotifier) recipients(ctx context.Context, textID, actorID string) ([]notify.Recipient, error) {
	subscribers, err := n.repo.SubscribersForText(textID)
	if err != nil {
		return nil, err
	}

	userIDs := make([]string, 0, len(subscribers))
	for _, userID := range subscribers {
		if userID != actorID {
			userIDs = append(userIDs, userID)
		}
	}
	if len(userIDs) == 0 {
		return nil, nil
	}

	// One query each rather than per recipient: a text with many followers
	// would otherwise cost two round trips per person.
	emails, err := n.store.EmailsForUsers(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	deviceTokens, err := n.store.DeviceTokensForUsers(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	recipients := make([]notify.Recipient, 0, len(userIDs))
	for _, userID := range userIDs {
		recipients = append(recipients, notify.Recipient{
			UserID:       userID,
			Email:        emails[userID],
			DeviceTokens: deviceTokens[userID],
		})
	}
	return recipients, nil
}

// buildDispatcher assembles the channels a deployment has actually
// configured. Every one of them is optional, and an unconfigured
// deployment gets the log channel alone — visible, inert, and impossible
// to mistake for a broken provider.
func buildDispatcher(store *Store, serviceAccountPath string) *notify.Dispatcher {
	channels := []notify.Channel{notify.LogChannel{}}

	push, err := notify.NewPushChannel(serviceAccountPath, func(token string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.DeleteDeviceToken(ctx, token); err != nil {
			log.Printf("notify: forgetting dead device token: %v", err)
		}
	})
	switch {
	case err != nil:
		// Misconfigured push is worth saying out loud, but not worth
		// refusing to start over: every other channel still works.
		log.Printf("notify: push channel disabled: %v", err)
	case push != nil:
		channels = append(channels, push)
	}

	if mailConfigured() {
		channels = append(channels, notify.NewEmailChannel(sendEmail))
	}

	// The inbox is the one channel that needs no configuring: the store is
	// always there, so every deployment gets a readable history even when
	// no push or mail provider is set up. It also flattens the
	// notification into columns here rather than in notify, which has no
	// business knowing what a text id is — the Data map is the contract
	// between the two (see notify.Notification).
	channels = append(channels, notify.NewInboxChannel(
		func(ctx context.Context, userIDs []string, n notify.Notification) error {
			return store.SaveNotifications(ctx, userIDs, n.Data["type"], n.Data["textId"], n.Title, n.Body)
		}))

	dispatcher := notify.NewDispatcher(channels...)
	log.Printf("notify: channels enabled: %v", dispatcher.Channels())
	return dispatcher
}
