package rbac

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestRootUserCanDoEverything(t *testing.T) {
	user := &User{Root: true}
	for _, action := range []Action{
		ActionVote, ActionCreateText, ActionCloseText,
		ActionSelect, ActionEditSelection, ActionUpdateText,
	} {
		if !user.Can(action) {
			t.Errorf("root user should be able to %s", action)
		}
	}
}

func TestNonRootUserRespectsPermissions(t *testing.T) {
	user := &User{Permissions: Permissions{CanVote: true}}
	if !user.Can(ActionVote) {
		t.Fatal("expected CanVote to allow ActionVote")
	}
	if user.Can(ActionCreateText) {
		t.Fatal("expected CanCreateText=false to deny ActionCreateText")
	}
}

func TestCreateGetListUpdateDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}

	user, err := store.CreateUser(false, Permissions{CanVote: true})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" {
		t.Fatal("expected a generated ID")
	}

	fetched, err := store.GetUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.ID != user.ID || !fetched.Permissions.CanVote {
		t.Fatalf("got %+v", fetched)
	}

	if _, err := store.CreateUser(true, Permissions{}); err != nil {
		t.Fatal(err)
	}
	if got := len(store.ListUsers()); got != 2 {
		t.Fatalf("expected 2 users, got %d", got)
	}

	updated, err := store.UpdateUser(user.ID, false, Permissions{CanCreateText: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Permissions.CanVote || !updated.Permissions.CanCreateText {
		t.Fatalf("update did not replace permissions: %+v", updated.Permissions)
	}

	if err := store.DeleteUser(user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetUser(user.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestUnknownUserOperations(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetUser("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := store.UpdateUser("does-not-exist", false, Permissions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := store.DeleteUser("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(true, Permissions{})
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fetched, err := reopened.GetUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !fetched.Root {
		t.Fatalf("expected Root to survive reopen, got %+v", fetched)
	}
}

func TestOpenMissingFileStartsEmpty(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "does-not-exist-yet.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(store.ListUsers()); got != 0 {
		t.Fatalf("expected empty directory, got %d users", got)
	}
}
