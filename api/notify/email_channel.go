package notify

import (
	"context"
	"log"
	"sync"
)

// maxConcurrentEmails bounds in-flight sends, for the same reason the push
// channel bounds its own: one transactional-email request per recipient,
// and a popular text should not open one connection per follower at once.
const maxConcurrentEmails = 4

// EmailChannel delivers notifications as plain-text email.
//
// The actual sending is injected rather than imported, so this package
// stays free of any particular provider — the api wires it to its existing
// Brevo client, and a test or another deployment can pass anything with
// the same shape.
type EmailChannel struct {
	send func(to, subject, body string) error
}

// NewEmailChannel returns nil when send is nil, so an unconfigured
// deployment simply has no email channel rather than one that fails on
// every notification.
func NewEmailChannel(send func(to, subject, body string) error) *EmailChannel {
	if send == nil {
		return nil
	}
	return &EmailChannel{send: send}
}

func (*EmailChannel) Name() string { return "email" }

func (e *EmailChannel) Send(_ context.Context, n Notification, recipients []Recipient) error {
	var wg sync.WaitGroup
	slots := make(chan struct{}, maxConcurrentEmails)

	for _, recipient := range recipients {
		if recipient.Email == "" {
			// No address for this person — not a failure, just someone
			// this channel cannot reach.
			continue
		}

		wg.Add(1)
		slots <- struct{}{}
		go func(recipient Recipient) {
			defer wg.Done()
			defer func() { <-slots }()

			// Logged, not returned: one bounced address must not stop the
			// rest of the fan-out.
			if err := e.send(recipient.Email, n.Title, n.Body); err != nil {
				log.Printf("notify(email): %s: %v", recipient.Email, err)
			}
		}(recipient)
	}

	wg.Wait()
	return nil
}
