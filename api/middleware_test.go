package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/etouraille/queel/rbac"
)

// TestCurrentPermissionsFallsBackToTheToken covers the last resort of the
// degraded path: the directory is unreachable *and* nothing was ever read
// for this caller, so there is nothing fresher than their token to use.
//
// Refusing instead would turn a directory hiccup into a site-wide lockout,
// and the token is precisely what the api trusted before any of this — so
// falling back is a return to the previous behaviour, not a hole opened for
// the occasion.
func TestCurrentPermissionsFallsBackToTheToken(t *testing.T) {
	// A real handle on an address nothing listens on: sql.Open doesn't
	// connect, so the failure surfaces on the query — which is the shape of
	// the outage this guards against. A Store with a nil db would panic
	// instead of erroring, and prove nothing about the fallback.
	db, err := sql.Open("postgres", "postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Store{db: db}

	claims := rbac.Claims{
		Subject:   "user-1",
		Root:      true,
		Perms:     rbac.Permissions{CanVote: true, CanSubscribe: true}.Bits(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	perms, root := currentPermissions(context.Background(), store, nil, newPermissionCache(), claims)

	if !perms.CanVote || !perms.CanSubscribe {
		t.Fatalf("permissions = %+v, want the token's own when the directory can't answer", perms)
	}
	if perms.CanCreateText {
		t.Fatal("fallback must return the token's rights, not more")
	}
	if !root {
		t.Fatal("root must survive the fallback too")
	}
}

// TestCurrentPermissionsPrefersTheCacheOverTheToken is the point of the
// cache: with the same unreachable directory, rights read at some earlier
// moment beat the token, which was signed at sign-in and never revisited.
//
// The two disagree here on purpose — the token still claims a right the
// directory had already taken away. Serving the token would re-grant it for
// the length of the outage.
func TestCurrentPermissionsPrefersTheCacheOverTheToken(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &Store{db: db}

	claims := rbac.Claims{
		Subject:   "user-1",
		Root:      true,
		Perms:     rbac.Permissions{CanVote: true, CanCreateText: true}.Bits(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	// What the directory last said, before it stopped answering: the right
	// to create was withdrawn, and so was root.
	cache := newPermissionCache()
	cache.remember("user-1", rbac.Permissions{CanVote: true}, false)

	perms, root := currentPermissions(context.Background(), store, nil, cache, claims)

	if !perms.CanVote {
		t.Fatalf("permissions = %+v, want the cached rights", perms)
	}
	if perms.CanCreateText {
		t.Fatal("a right the directory had withdrawn must not come back from the token")
	}
	if root {
		t.Fatal("root must come from the cache too, not from the token")
	}

	// Somebody the cache has never seen still gets their token, so one
	// unknown caller doesn't lose everything.
	stranger := rbac.Claims{Subject: "user-2", Perms: rbac.Permissions{CanVote: true}.Bits()}
	if perms, _ := currentPermissions(context.Background(), store, nil, cache, stranger); !perms.CanVote {
		t.Fatalf("an uncached caller = %+v, want their token's rights", perms)
	}
}

// TestRequireTokenRejectsWithoutABearer pins the gate itself: the refresh
// added around it must not have made an unauthenticated request reach a
// handler.
func TestRequireTokenRejectsWithoutABearer(t *testing.T) {
	reached := false
	// Stores are never reached: the request is turned away before any
	// lookup, which is the point.
	handler := requireToken([]byte("secret"), nil, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if reached {
		t.Fatal("the handler must not run without a token")
	}
}
