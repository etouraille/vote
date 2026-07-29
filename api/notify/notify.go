// Package notify fans one event out to several delivery channels.
//
// The point of the indirection is that "who should hear about this" and
// "how does it reach them" are separate concerns that change at different
// rates: call sites name an audience and an event, and adding email or a
// webhook alongside push later touches only this package.
//
// A Channel receives the whole recipient list at once rather than being
// called per recipient, so a transport that can batch gets to do it once
// instead of being driven one recipient at a time from outside.
package notify

import (
	"context"
	"log"
	"sync"
)

// Recipient is one person to reach, with everything the channels might
// need to do so. A channel simply skips anyone it has no address for: a
// user with no registered device is not an error for the push channel,
// just someone it cannot reach.
type Recipient struct {
	UserID string
	Email  string
	// DeviceTokens is every device this user has registered — someone
	// signed in on a phone and a tablet gets both.
	DeviceTokens []string
}

// Notification is what happened, in terms a channel can render. Title and
// Body are for humans; Data is the machine-readable payload a mobile app
// uses to deep-link, and is ignored by channels that have no equivalent.
type Notification struct {
	Title string
	Body  string
	Data  map[string]string
}

// Channel is one way of delivering a notification. Implementations must be
// safe for concurrent use: Dispatcher calls them in parallel.
type Channel interface {
	// Name identifies the channel in logs.
	Name() string

	// Send delivers to everyone it can reach. Returning an error means the
	// channel as a whole failed; being unable to reach one recipient is
	// not an error, it is the normal case for anyone this channel has no
	// address for.
	Send(ctx context.Context, n Notification, recipients []Recipient) error
}

// Dispatcher sends one notification through every configured channel.
//
// Channels are independent: one failing must not stop the others, so a
// failure is logged and the rest proceed. Notifying is a side effect of
// whatever the user actually asked for — nobody's text edit should fail
// because a push provider was down.
type Dispatcher struct {
	channels []Channel
}

// NewDispatcher builds a dispatcher over the channels that are actually
// configured. Passing none is valid and makes every Notify call a no-op —
// the shape a deployment that hasn't set up any provider takes.
func NewDispatcher(channels ...Channel) *Dispatcher {
	enabled := make([]Channel, 0, len(channels))
	for _, channel := range channels {
		if channel != nil {
			enabled = append(enabled, channel)
		}
	}
	return &Dispatcher{channels: enabled}
}

// Channels lists the configured channel names, for logging at startup what
// a deployment will actually be able to deliver.
func (d *Dispatcher) Channels() []string {
	names := make([]string, 0, len(d.channels))
	for _, channel := range d.channels {
		names = append(names, channel.Name())
	}
	return names
}

// Notify delivers n to recipients over every channel, in parallel, and
// waits for them all.
//
// It never returns an error: there is no useful way for a caller to react
// to "the email channel was down", and propagating it would tempt call
// sites into failing the user's actual request over it. Failures are
// logged instead.
//
// An empty recipient list short-circuits — the common case once the actor
// has been excluded from their own text's followers.
func (d *Dispatcher) Notify(ctx context.Context, n Notification, recipients []Recipient) {
	if len(d.channels) == 0 || len(recipients) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, channel := range d.channels {
		wg.Add(1)
		go func(channel Channel) {
			defer wg.Done()
			if err := channel.Send(ctx, n, recipients); err != nil {
				log.Printf("notify: channel %s failed: %v", channel.Name(), err)
			}
		}(channel)
	}
	wg.Wait()
}
