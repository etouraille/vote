package rbac

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func startTestSocket(t *testing.T) net.Conn {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}

	socketPath := filepath.Join(t.TempDir(), "rbac.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ready := make(chan struct{})
	go func() {
		// ServeSocket creates the listener synchronously before Accept
		// loops; give it a moment then signal readiness by dialing.
		close(ready)
		_ = ServeSocket(ctx, socketPath, store)
	}()
	<-ready

	var conn net.Conn
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			return conn
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("could not connect to rbac socket: %v", err)
	return nil
}

func roundTrip(t *testing.T, conn net.Conn, req Request) Response {
	t.Helper()

	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatalf("no response from socket: %v", scanner.Err())
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestSocketCreateGetListUpdateDelete(t *testing.T) {
	conn := startTestSocket(t)
	defer conn.Close()

	created := roundTrip(t, conn, Request{Action: "create", Permissions: Permissions{CanVote: true}})
	if !created.OK || created.User == nil {
		t.Fatalf("create failed: %+v", created)
	}
	id := created.User.ID

	got := roundTrip(t, conn, Request{Action: "get", ID: id})
	if !got.OK || got.User.ID != id {
		t.Fatalf("get failed: %+v", got)
	}

	listed := roundTrip(t, conn, Request{Action: "list"})
	if !listed.OK || len(listed.Users) != 1 {
		t.Fatalf("list failed: %+v", listed)
	}

	updated := roundTrip(t, conn, Request{Action: "update", ID: id, Root: true})
	if !updated.OK || !updated.User.Root {
		t.Fatalf("update failed: %+v", updated)
	}

	deleted := roundTrip(t, conn, Request{Action: "delete", ID: id})
	if !deleted.OK {
		t.Fatalf("delete failed: %+v", deleted)
	}

	afterDelete := roundTrip(t, conn, Request{Action: "get", ID: id})
	if afterDelete.OK {
		t.Fatalf("expected get to fail after delete, got %+v", afterDelete)
	}
}

func TestSocketUnknownAction(t *testing.T) {
	conn := startTestSocket(t)
	defer conn.Close()

	resp := roundTrip(t, conn, Request{Action: "explode"})
	if resp.OK {
		t.Fatalf("expected failure for unknown action, got %+v", resp)
	}
}
