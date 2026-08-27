package notify

import "context"

// InboxChannel persists notifications so they can be read back later,
// rather than only pushed at the moment they happen.
//
// It is a channel like any other on purpose: an inbox is just one more way
// of delivering the same event, and modelling it as such means the fan-out
// doesn't have to know it exists. It also makes the history identical
// wherever the user reads it, which no per-device store can promise.
//
// The persistence itself is injected, so this package keeps knowing
// nothing about Postgres — same arrangement as EmailChannel and its mailer.
type InboxChannel struct {
	save func(ctx context.Context, userIDs []string, n Notification) error
}

// NewInboxChannel returns nil when save is nil, so a deployment without a
// store simply has no inbox rather than one that fails on every send.
func NewInboxChannel(save func(ctx context.Context, userIDs []string, n Notification) error) *InboxChannel {
	if save == nil {
		return nil
	}
	return &InboxChannel{save: save}
}

func (*InboxChannel) Name() string { return "inbox" }

// Send stores one entry per recipient.
//
// Unlike push and email, the error is returned rather than swallowed: a
// failed write means the event is lost for good, where a failed push only
// means this particular device missed a message it can still find in the
// inbox. Dispatcher logs it without disturbing the other channels.
func (c *InboxChannel) Send(ctx context.Context, n Notification, recipients []Recipient) error {
	userIDs := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		userIDs = append(userIDs, recipient.UserID)
	}
	return c.save(ctx, userIDs, n)
}
