package rbac

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestRootUserCanDoEverything(t *testing.T) {
	user := &User{Root: true}
	for _, action := range []Action{
		ActionVote, ActionCreateText, ActionCloseText,
		ActionEditText, ActionUpdateText, ActionSubscribe,
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

func TestCreateUserWithIDIsIdempotent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateUserWithID("root-uuid", true, Permissions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUserWithID("root-uuid", true, Permissions{}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists bootstrapping the same UUID twice (see api/main.go's QUEEL_ROOT_UUID handling), got %v", err)
	}
}

// TestTwoStoreHandlesSharingOneDatabaseFileStayConsistent is the scenario
// this migration exists for: several cluster nodes on one machine, each its
// own process (here: its own *Store, its own *sql.DB connection), pointed
// at the same QUEEL_RBAC_PATH. The flat-JSON-file Store this replaced was
// never actually safe for that — no cross-process coordination beyond an
// in-process mutex, and a "rewrite the whole file" write under a concurrent
// writer elsewhere could corrupt it.
func TestTwoStoreHandlesSharingOneDatabaseFileStayConsistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")

	storeA, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storeA.Close() })

	storeB, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storeB.Close() })

	user, err := storeA.CreateUser(true, Permissions{CanVote: true})
	if err != nil {
		t.Fatal(err)
	}

	fetched, err := storeB.GetUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !fetched.Root || !fetched.Permissions.CanVote {
		t.Fatalf("storeB should see what storeA wrote to the shared file, got %+v", fetched)
	}

	if _, err := storeB.UpdateUser(user.ID, false, Permissions{CanCreateText: true}); err != nil {
		t.Fatal(err)
	}
	reFetched, err := storeA.GetUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reFetched.Root || !reFetched.Permissions.CanCreateText {
		t.Fatalf("storeA should see storeB's update, got %+v", reFetched)
	}
}

// TestConcurrentCreatesDontCorruptOrDuplicate hammers one Store from many
// goroutines at once — the in-process equivalent of many concurrent
// requests hitting PUT /api/admin/users/{id}/permissions. Every create must
// either succeed cleanly or fail with a real error; none may be silently
// lost or duplicated the way a "read whole file, mutate, write whole file"
// race could before.
func TestConcurrentCreatesDontCorruptOrDuplicate(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.CreateUser(false, Permissions{}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent CreateUser failed: %v", err)
	}

	if got := len(store.ListUsers()); got != n {
		t.Fatalf("expected %d users after %d concurrent creates, got %d", n, n, got)
	}
}

// TestPermissionsReadsLegacyEditingKeys covers the one way merging
// canSelect and canEditSelection into canEditText could have gone wrong: a
// stored row written before the merge would stop matching any field, and
// every account granted the right to edit would come back unable to — no
// error, no trace, just silently stripped.
func TestPermissionsReadsLegacyEditingKeys(t *testing.T) {
	cases := []struct {
		name string
		json string
		want bool
	}{
		{"legacy select", `{"canSelect":true}`, true},
		{"legacy edit-selection", `{"canEditSelection":true}`, true},
		{"legacy both", `{"canSelect":true,"canEditSelection":true}`, true},
		{"legacy neither", `{"canSelect":false,"canEditSelection":false}`, false},
		{"current key", `{"canEditText":true}`, true},
		{"absent entirely", `{"canVote":true}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var perms Permissions
			if err := json.Unmarshal([]byte(tc.json), &perms); err != nil {
				t.Fatal(err)
			}
			if perms.CanEditText != tc.want {
				t.Fatalf("CanEditText = %v, want %v (from %s)", perms.CanEditText, tc.want, tc.json)
			}
		})
	}

	// The other fields must survive the custom unmarshaller — the usual
	// trap when one is written by hand.
	var perms Permissions
	if err := json.Unmarshal([]byte(`{"canVote":true,"canSubscribe":true,"canUpdateText":true}`), &perms); err != nil {
		t.Fatal(err)
	}
	if !perms.CanVote || !perms.CanSubscribe || !perms.CanUpdateText {
		t.Fatalf("unrelated fields lost: %+v", perms)
	}
}

// TestPermissionBitsAreFrozen pins the numeric values a signed token
// carries. Merging two rights removed a bit from the middle; had the block
// stayed on iota, everything above it would have shifted down and every
// token already issued would have decoded as a different set of rights.
func TestPermissionBitsAreFrozen(t *testing.T) {
	for _, tc := range []struct {
		bit  PermBit
		want PermBit
	}{
		{PermVote, 1},
		{PermCreateText, 2},
		{PermCloseText, 4},
		{PermEditText, 8},
		{PermUpdateText, 32},
		{PermSubscribe, 64},
	} {
		if tc.bit != tc.want {
			t.Errorf("bit = %d, want %d — moving one invalidates tokens already signed", tc.bit, tc.want)
		}
	}
}

// TestPermBitReadsATokenSignedAsAUint8 is what makes widening PermBit safe
// to do at all: the mask crosses the wire as a JSON number, so a token
// signed while it was a uint8 has to decode into the wider type unchanged.
// Were it ever encoded as a fixed-width value instead, this is the test
// that would fail.
func TestPermBitReadsATokenSignedAsAUint8(t *testing.T) {
	// A mask written when PermBit was a byte: vote + editText + subscribe.
	const signedWhenNarrow = `{"sub":"user-1","perms":73,"exp":0}`

	var claims Claims
	if err := json.Unmarshal([]byte(signedWhenNarrow), &claims); err != nil {
		t.Fatal(err)
	}

	perms := PermissionsFromBits(claims.Perms)
	if !perms.CanVote || !perms.CanEditText || !perms.CanSubscribe {
		t.Fatalf("permissions = %+v, want vote + editText + subscribe", perms)
	}
	if perms.CanCreateText || perms.CanCloseText || perms.CanUpdateText {
		t.Fatalf("permissions = %+v, want nothing beyond those three", perms)
	}

	// And the wider type actually reaches past the old ceiling.
	wide := PermBit(1 << 15)
	if wide == 0 {
		t.Fatal("PermBit must hold a bit above position 7")
	}
}
