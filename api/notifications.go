package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/etouraille/queel"
	"vote-api/notify"
)

// notifyTimeout bounds a fan-out. It runs detached from the request that
// triggered it (see textNotifier.notify), so nothing else would ever stop
// it.
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

// EditProposed notifies the followers of a text somebody has proposed a
// change to — which is what modifying a text means here, now that the route
// overwriting one outright is gone: carving out a zone and submitting a
// competing wording for it, for the followers to vote on.
func (n *textNotifier) EditProposed(textID, title, actorID string) {
	n.notify(textID, actorID, func(actor string) notify.Notification {
		body := fmt.Sprintf("Une modification vient d'être proposée sur « %s ».", title)
		if actor != "" {
			body = fmt.Sprintf("%s a proposé une modification sur « %s ».", actor, title)
		}
		return notify.Notification{
			Title: "Modification proposée",
			Body:  body,
			Data:  eventData("text.edit-proposed", textID, actor),
		}
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
	n.notify(text.ID, actorID, func(actor string) notify.Notification {
		body := fmt.Sprintf("Le vote sur « %s » est clos, la nouvelle version est disponible.", text.Title)
		if actor != "" {
			body = fmt.Sprintf("%s a clos le vote sur « %s », la nouvelle version est disponible.", actor, text.Title)
		}
		return notify.Notification{
			Title: "Tour de vote clos",
			Body:  body,
			// Not edit-proposed: there is no round open on a freshly forked
			// text, so a client following this lands on the text itself
			// rather than on an empty vote page.
			Data: eventData("text.round-closed", text.ID, actor),
		}
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
func (n *textNotifier) notify(textID, actorID string, build func(actor string) notify.Notification) {
	if n == nil || n.dispatcher == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		n.deliver(ctx, textID, actorID, build(n.actorName(ctx, actorID)))
	}()
}

// actorName is who to name in a notification's wording: the pseudo if one
// was set, otherwise the local part of the email — the same fallback the
// front ends already display.
//
// Empty when there is nobody to name, and every wording then drops the name
// rather than printing a blank. That covers a scheduled close carried out
// by no one, and a lookup that failed: naming is a courtesy, and losing it
// must not cost the notification itself.
func (n *textNotifier) actorName(ctx context.Context, actorID string) string {
	if actorID == "" {
		return ""
	}

	user, err := n.store.UserByID(ctx, actorID)
	if err != nil {
		log.Printf("notify: naming the author of an event (%s): %v", actorID, err)
		return ""
	}
	if user.Pseudo != nil && *user.Pseudo != "" {
		return *user.Pseudo
	}
	if local, _, found := strings.Cut(user.Email, "@"); found && local != "" {
		return local
	}
	return user.Email
}

// eventData is the machine-readable half of a notification: what happened,
// to which text, and — when there is one to name — who did it. Clients
// render the body as it comes, but carrying the name separately lets one
// build its own wording without parsing a sentence.
func eventData(eventType, textID, actor string) map[string]string {
	data := map[string]string{
		"type":   eventType,
		"textId": textID,
	}
	if actor != "" {
		data["actor"] = actor
	}
	return data
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

		actor := n.actorName(ctx, actorID)
		body := fmt.Sprintf("Un vote vient d'être déposé sur « %s ».", text.Title)
		if actor != "" {
			body = fmt.Sprintf("%s vient de voter sur « %s ».", actor, text.Title)
		}

		n.deliver(ctx, text.ID, actorID, notify.Notification{
			Title: "Nouveau vote",
			Body:  body,
			// A vote can only be cast while a round is open, so the round
			// is where a tapped notification should land.
			Data: eventData("text.vote-cast", text.ID, actor),
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
			return store.SaveNotifications(ctx, userIDs, n.Data["type"], n.Data["textId"], n.Title, n.Body, n.Data["actor"])
		}))

	dispatcher := notify.NewDispatcher(channels...)
	log.Printf("notify: channels enabled: %v", dispatcher.Channels())
	return dispatcher
}
