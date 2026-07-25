package rbac

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
)

// Request is one line of the socket protocol: newline-delimited JSON in,
// newline-delimited JSON out, one Request/Response pair per line. Deliberately
// plain text over a local-only Unix socket rather than HTTP — there's
// nothing here that needs a network-facing surface, and this keeps the
// protocol trivial to drive by hand (e.g. `socat - UNIX-CONNECT:...`) for
// emergency access.
type Request struct {
	Action      string      `json:"action"`
	ID          string      `json:"id,omitempty"`
	Root        bool        `json:"root,omitempty"`
	Permissions Permissions `json:"permissions,omitempty"`
}

type Response struct {
	OK    bool    `json:"ok"`
	Error string  `json:"error,omitempty"`
	User  *User   `json:"user,omitempty"`
	Users []*User `json:"users,omitempty"`
}

// ServeSocket listens on a Unix domain socket at socketPath and serves the
// admin protocol against store until ctx is canceled. The socket is created
// mode 0600 — only the user running this process (the "super utilisateur")
// can connect at all; there is no further authentication because the
// filesystem permission on the socket itself is the access control.
func ServeSocket(ctx context.Context, socketPath string, store *Store) error {
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		return err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		go handleConn(conn, store)
	}
}

func handleConn(conn net.Conn, store *Store) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	encoder := json.NewEncoder(conn)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = encoder.Encode(Response{OK: false, Error: "invalid request: " + err.Error()})
			continue
		}
		_ = encoder.Encode(handle(store, req))
	}
}

func handle(store *Store, req Request) Response {
	switch req.Action {
	case "create":
		user, err := store.CreateUser(req.Root, req.Permissions)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, User: user}

	case "get":
		user, err := store.GetUser(req.ID)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, User: user}

	case "list":
		return Response{OK: true, Users: store.ListUsers()}

	case "update":
		user, err := store.UpdateUser(req.ID, req.Root, req.Permissions)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, User: user}

	case "delete":
		if err := store.DeleteUser(req.ID); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true}

	default:
		return Response{OK: false, Error: "unknown action: " + req.Action}
	}
}
