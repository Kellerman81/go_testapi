package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore returns a memory-backed store with no seed data.
func newTestStore() *Store {
	return NewStore("", nil)
}

// newFileStore returns a file-backed store rooted in a temp directory.
// The caller is responsible for calling cleanup().
func newFileStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	return NewStore(dir, nil), func() { os.RemoveAll(dir) }
}

// ---- Seed ----

func TestStore_Seed_PopulatesUsers(t *testing.T) {
	s := newTestStore()
	s.Seed()
	users := s.List()
	if len(users) == 0 {
		t.Fatal("expected seed data, got none")
	}
}

func TestStore_Seed_SkipsWhenNotEmpty(t *testing.T) {
	s := newTestStore()
	s.Seed()
	first := len(s.List())
	s.Seed() // second call should be a no-op
	if got := len(s.List()); got != first {
		t.Fatalf("second Seed() changed count: %d → %d", first, got)
	}
}

// ---- Create ----

func TestStore_Create_OK(t *testing.T) {
	s := newTestStore()
	u, err := s.Create(User{Username: "alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == "" {
		t.Error("expected non-empty ID")
	}
	if u.Username != "alice" {
		t.Errorf("username: got %q, want %q", u.Username, "alice")
	}
	if u.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if u.Permissions == nil {
		t.Error("expected Permissions to be initialised (not nil)")
	}
}

func TestStore_Create_DuplicateUsername(t *testing.T) {
	s := newTestStore()
	if _, err := s.Create(User{Username: "alice"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := s.Create(User{Username: "alice"}); err != ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

// ---- Get ----

func TestStore_Get_OK(t *testing.T) {
	s := newTestStore()
	created, _ := s.Create(User{Username: "bob"})
	got, err := s.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, created.ID)
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	s := newTestStore()
	if _, err := s.Get("nonexistent"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- Update ----

func TestStore_Update_Fields(t *testing.T) {
	s := newTestStore()
	u, _ := s.Create(User{Username: "charlie", Email: "old@example.com", FirstName: "Old", LastName: "Name"})

	time.Sleep(20 * time.Millisecond)
	updated, err := s.Update(u.ID, User{
		Username:  "charlie2",
		Email:     "new@example.com",
		FirstName: "New",
		LastName:  "Name2",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Username != "charlie2" {
		t.Errorf("Username: got %q", updated.Username)
	}
	if updated.Email != "new@example.com" {
		t.Errorf("Email: got %q", updated.Email)
	}
	if updated.UpdatedAt == u.UpdatedAt {
		t.Error("expected UpdatedAt to change")
	}
}

func TestStore_Update_EmptyFieldsPreserved(t *testing.T) {
	s := newTestStore()
	u, _ := s.Create(User{Username: "diana", Email: "diana@example.com"})
	updated, err := s.Update(u.ID, User{Email: "new@example.com"}) // leave Username empty
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Username != "diana" {
		t.Errorf("Username should be unchanged, got %q", updated.Username)
	}
}

func TestStore_Update_DuplicateUsername(t *testing.T) {
	s := newTestStore()
	s.Create(User{Username: "eve"})
	frank, _ := s.Create(User{Username: "frank"})
	if _, err := s.Update(frank.ID, User{Username: "eve"}); err != ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestStore_Update_NotFound(t *testing.T) {
	s := newTestStore()
	if _, err := s.Update("nonexistent", User{Email: "x@x.com"}); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- SetEnabled ----

func TestStore_SetEnabled(t *testing.T) {
	s := newTestStore()
	u, _ := s.Create(User{Username: "grace"})

	got, err := s.SetEnabled(u.ID, false)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if got.Enabled {
		t.Error("expected Enabled=false")
	}

	got, err = s.SetEnabled(u.ID, true)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if !got.Enabled {
		t.Error("expected Enabled=true")
	}
}

func TestStore_SetEnabled_NotFound(t *testing.T) {
	s := newTestStore()
	if _, err := s.SetEnabled("nonexistent", true); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- Delete ----

func TestStore_Delete_OK(t *testing.T) {
	s := newTestStore()
	u, _ := s.Create(User{Username: "henry"})
	if err := s.Delete(u.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(u.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestStore_Delete_NotFound(t *testing.T) {
	s := newTestStore()
	if err := s.Delete("nonexistent"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- Permissions ----

func TestStore_AddPermission_Idempotent(t *testing.T) {
	s := newTestStore()
	u, _ := s.Create(User{Username: "ivan"})

	perms, err := s.AddPermission(u.ID, "read")
	if err != nil {
		t.Fatalf("AddPermission: %v", err)
	}
	if len(perms) != 1 {
		t.Fatalf("expected 1 permission, got %d", len(perms))
	}

	// add again — should stay at 1
	perms, _ = s.AddPermission(u.ID, "read")
	if len(perms) != 1 {
		t.Errorf("idempotent: expected 1 permission, got %d", len(perms))
	}
}

func TestStore_AddRemovePermission(t *testing.T) {
	s := newTestStore()
	u, _ := s.Create(User{Username: "julia"})

	s.AddPermission(u.ID, "read")
	s.AddPermission(u.ID, "write")

	perms, _ := s.GetPermissions(u.ID)
	if len(perms) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(perms))
	}

	perms, err := s.RemovePermission(u.ID, "write")
	if err != nil {
		t.Fatalf("RemovePermission: %v", err)
	}
	if len(perms) != 1 || perms[0] != "read" {
		t.Errorf("after remove: got %v, want [read]", perms)
	}
}

func TestStore_Permission_NotFound(t *testing.T) {
	s := newTestStore()
	if _, err := s.AddPermission("nonexistent", "read"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := s.RemovePermission("nonexistent", "read"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetPermissions("nonexistent"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- File persistence ----

func TestStore_FilePersistence_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Create a user in store1, then close it.
	store1 := NewStore(dir, nil)
	u, err := store1.Create(User{Username: "persisted", Email: "p@example.com", FirstName: "Per", LastName: "Sisted"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store1.AddPermission(u.ID, "admin")

	// Verify the file was written.
	if _, err := os.Stat(filepath.Join(dir, "users.json")); err != nil {
		t.Fatalf("users.json not created: %v", err)
	}

	// Open a fresh store from the same directory — it should reload the data.
	store2 := NewStore(dir, nil)
	loaded, err := store2.Get(u.ID)
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if loaded.Username != "persisted" {
		t.Errorf("Username: got %q, want %q", loaded.Username, "persisted")
	}
	if len(loaded.Permissions) != 1 || loaded.Permissions[0] != "admin" {
		t.Errorf("Permissions after reload: got %v", loaded.Permissions)
	}
}

func TestStore_FilePersistence_SeedSkippedWhenDataExists(t *testing.T) {
	dir := t.TempDir()

	store1 := NewStore(dir, nil)
	store1.Create(User{Username: "existing"})

	store2 := NewStore(dir, nil)
	store2.Seed() // should not add the demo users because the store already has data
	users := store2.List()
	if len(users) != 1 {
		t.Errorf("expected 1 user (not seeded), got %d", len(users))
	}
}
