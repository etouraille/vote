package notify

import (
	"context"
	"log"
)

// LogChannel writes notifications to the process log instead of delivering
// them anywhere.
//
// It exists so a deployment with no provider configured still shows that
// the fan-out fired, and to whom — the difference between "notifications
// aren't set up" and "notifications are broken" is otherwise invisible,
// and that ambiguity costs far more to debug than this costs to keep.
type LogChannel struct{}

func (LogChannel) Name() string { return "log" }

func (LogChannel) Send(_ context.Context, n Notification, recipients []Recipient) error {
	userIDs := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		userIDs = append(userIDs, recipient.UserID)
	}
	log.Printf("notify(log): %q — %q → %v", n.Title, n.Body, userIDs)
	return nil
}
