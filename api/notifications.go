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

		recipients, err := n.recipients(ctx, textID, actorID)
		if err != nil {
			log.Printf("notify: resolving recipients for text %s: %v", textID, err)
			return
		}

		n.dispatcher.Notify(ctx, notification, recipients)
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

	dispatcher := notify.NewDispatcher(channels...)
	log.Printf("notify: channels enabled: %v", dispatcher.Channels())
	return dispatcher
}
