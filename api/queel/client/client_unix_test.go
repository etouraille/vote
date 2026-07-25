//go:build !windows

package client_test

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/client"
	"github.com/etouraille/queel/server"
)

func newUnixSocketTestClient(t *testing.T) *client.Client {
	t.Helper()
	engine, err := queel.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	repo := queel.NewRepository(engine)

	socketPath := filepath.Join(t.TempDir(), "queel.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: server.NewHandler(repo, nil)}
	go srv.Serve(listener)
	t.Cleanup(func() { _ = srv.Close() })

	return client.NewUnixSocket(socketPath)
}

func TestClientOverUnixSocket(t *testing.T) {
	ctx := context.Background()
	c := newUnixSocketTestClient(t)

	text, err := c.CreateText(ctx, "Constitution", "Nous le peuple.", "creator")
	if err != nil {
		t.Fatal(err)
	}

	fetched, err := c.Text(ctx, text.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Content != "Nous le peuple." {
		t.Fatalf("Content = %q", fetched.Content)
	}
}
