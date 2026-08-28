// Package client is the Go connector for a running queel server. Any Go
// project — this repo's own api module or any other — imports only this
// package (and queel itself for the shared model types) to act on queel: it
// never touches the storage engine or the server's HTTP routes directly, and
// queel never has to know anything about the caller's own HTTP stack.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/etouraille/queel"
)

// Client talks to a queel server over HTTP; every method is one round trip.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates a connector for the queel server at baseURL (e.g.
// "http://localhost:9090"), using http.DefaultClient.
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: http.DefaultClient}
}

// NewUnixSocket creates a connector for a queel server listening on a Unix
// domain socket at socketPath (queeld with QUEEL_SOCKET set), rather than
// over TCP. Every method behaves identically to a New client; only the
// transport differs — the host in requests is a placeholder, since the
// dialer always connects to socketPath regardless of it.
func NewUnixSocket(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		baseURL: "http://unix",
		http:    &http.Client{Transport: transport},
	}
}

// APIError is returned when the server responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("queel: %s (status %d)", e.Message, e.StatusCode)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		message := errBody.Error
		if message == "" {
			message = resp.Status
		}
		return &APIError{StatusCode: resp.StatusCode, Message: message}
	}

	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// CreateText creates a new text from its initial content, attributed to
// authorID (see queel.Text.CreatedBy).
func (c *Client) CreateText(ctx context.Context, title, content, authorID string) (*queel.Text, error) {
	var text queel.Text
	body := map[string]string{"title": title, "content": content, "authorId": authorID}
	if err := c.do(ctx, http.MethodPost, "/texts", body, &text); err != nil {
		return nil, err
	}
	return &text, nil
}

// RecentTexts fetches up to limit texts starting after the first offset of
// them, most recently created first. limit <= 0 lets the server apply its
// own default; offset <= 0 starts from the beginning.
func (c *Client) RecentTexts(ctx context.Context, limit, offset int) ([]*queel.Text, error) {
	path := "/texts"
	query := ""
	if limit > 0 {
		query += "limit=" + strconv.Itoa(limit)
	}
	if offset > 0 {
		if query != "" {
			query += "&"
		}
		query += "offset=" + strconv.Itoa(offset)
	}
	if query != "" {
		path += "?" + query
	}
	var texts []*queel.Text
	if err := c.do(ctx, http.MethodGet, path, nil, &texts); err != nil {
		return nil, err
	}
	return texts, nil
}

// Text fetches a text by ID.
func (c *Client) Text(ctx context.Context, id string) (*queel.Text, error) {
	var text queel.Text
	if err := c.do(ctx, http.MethodGet, "/texts/"+id, nil, &text); err != nil {
		return nil, err
	}
	return &text, nil
}

// TextWithSlots fetches a text together with the slots of its current
// round, if any — one round trip instead of a separate Text + CurrentRound
// call. No open round isn't an error: RoundNumber is 0 and Slots is empty.
func (c *Client) TextWithSlots(ctx context.Context, id string) (*queel.TextWithSlots, error) {
	var result queel.TextWithSlots
	if err := c.do(ctx, http.MethodGet, "/texts/"+id+"/with-slots", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ProposeEdit selects a range [start,end) of the text's current content and
// proposes replacing it with content.
func (c *Client) ProposeEdit(ctx context.Context, textID string, start, end int, content, authorID string) (*queel.Fragment, error) {
	var fragment queel.Fragment
	body := map[string]any{"start": start, "end": end, "content": content, "authorId": authorID}
	if err := c.do(ctx, http.MethodPost, "/texts/"+textID+"/propose-edit", body, &fragment); err != nil {
		return nil, err
	}
	return &fragment, nil
}

// Subscribe makes userID follow textID.
//
// More than a preference now that acting on a text requires following it
// (see the server's checkSubscription): without this, a caller could
// propose an edit only on texts it had created itself, which CreateText
// subscribes it to.
//
// Idempotent server-side — subscribing to a text already followed
// succeeds, so a caller never has to check first.
func (c *Client) Subscribe(ctx context.Context, textID, userID string) error {
	body := map[string]string{"userId": userID}
	return c.do(ctx, http.MethodPost, "/texts/"+textID+"/subscribe", body, nil)
}

// CurrentRound returns the open voting round for a text, if any.
func (c *Client) CurrentRound(ctx context.Context, textID string) (*queel.Round, error) {
	var round queel.Round
	if err := c.do(ctx, http.MethodGet, "/texts/"+textID+"/round", nil, &round); err != nil {
		return nil, err
	}
	return &round, nil
}

// CloseRound finalizes the current voting round of a text and returns the
// updated text along with how each slot was resolved.
func (c *Client) CloseRound(ctx context.Context, textID string) (*queel.RoundOutcome, error) {
	var outcome queel.RoundOutcome
	if err := c.do(ctx, http.MethodPost, "/texts/"+textID+"/close-round", nil, &outcome); err != nil {
		return nil, err
	}
	return &outcome, nil
}

// Fragments lists every candidate fragment proposed for a given slot.
func (c *Client) Fragments(ctx context.Context, textID, slotID string) ([]*queel.Fragment, error) {
	var fragments []*queel.Fragment
	if err := c.do(ctx, http.MethodGet, "/texts/"+textID+"/slots/"+slotID+"/fragments", nil, &fragments); err != nil {
		return nil, err
	}
	return fragments, nil
}

// Fragment fetches a single fragment by ID.
func (c *Client) Fragment(ctx context.Context, id string) (*queel.Fragment, error) {
	var fragment queel.Fragment
	if err := c.do(ctx, http.MethodGet, "/fragments/"+id, nil, &fragment); err != nil {
		return nil, err
	}
	return &fragment, nil
}

// CastVote records that userID votes for fragmentID.
func (c *Client) CastVote(ctx context.Context, fragmentID, userID string) error {
	body := map[string]string{"fragmentId": fragmentID, "userId": userID}
	return c.do(ctx, http.MethodPost, "/vote", body, nil)
}

// VoteCount returns how many users currently have fragmentID as their active
// vote.
func (c *Client) VoteCount(ctx context.Context, fragmentID string) (int, error) {
	var result struct {
		Votes int `json:"votes"`
	}
	if err := c.do(ctx, http.MethodGet, "/fragments/"+fragmentID+"/votes", nil, &result); err != nil {
		return 0, err
	}
	return result.Votes, nil
}
