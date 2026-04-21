package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ---- domain model ----

// User is the core entity managed by the store.
type User struct {
	ID          string         `json:"id"                    xml:"Id"`
	Username    string         `json:"username"              xml:"Username"`
	Email       string         `json:"email"                 xml:"Email"`
	FirstName   string         `json:"first_name"            xml:"FirstName"`
	LastName    string         `json:"last_name"             xml:"LastName"`
	Enabled     bool           `json:"enabled"               xml:"Enabled"`
	Manager     string         `json:"manager"               xml:"Manager"`
	Permissions []string       `json:"permissions"           xml:"Permissions>Permission,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"  xml:"-"`
	CreatedAt   time.Time      `json:"created_at"            xml:"CreatedAt"`
	UpdatedAt   time.Time      `json:"updated_at"            xml:"UpdatedAt"`
}

// UserList wraps a slice for XML serialisation (needs a root element).
type UserList struct {
	Users []User `json:"users" xml:"User"`
}

// ---- errors ----

var (
	ErrNotFound      = errors.New("user not found")
	ErrAlreadyExists = errors.New("username already exists")
)

// ---- store ----

// Store is a thread-safe user store with optional file persistence.
type Store struct {
	mu           sync.RWMutex
	users        map[string]*User // keyed by ID
	dataDir      string           // empty = memory-only
	customFields map[string]any   // default attributes for new users
}

// NewStore returns a store. When dataDir is non-empty it will load existing
// data from disk and persist every mutation back to disk.
// customFields defines the attribute keys and default values seeded into every new user.
func NewStore(dataDir string, customFields map[string]any) *Store {
	s := &Store{users: make(map[string]*User), dataDir: dataDir, customFields: customFields}
	if dataDir != "" {
		var users []User
		if err := loadJSON(s.filePath(), &users); err != nil {
			logPersistErr("users load", err)
		}
		for i := range users {
			u := users[i]
			s.users[u.ID] = &u
		}
	}
	return s
}

// filePath returns the path of the users JSON file.
func (s *Store) filePath() string {
	return filepath.Join(s.dataDir, "users.json")
}

// persist writes the current user map to disk.
// Must be called while s.mu write lock is held.
func (s *Store) persist() {
	if s.dataDir == "" {
		return
	}
	users := make([]User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, *u)
	}
	logPersistErr("users save", saveJSON(s.filePath(), users))
}

// Seed populates the store with demo users only when the store is empty.
func (s *Store) Seed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.users) > 0 {
		return // already has data (loaded from disk)
	}
	demo := []User{
		{Username: "alice", Email: "alice@example.com", FirstName: "Alice", LastName: "Smith",
			Enabled: true, Permissions: []string{"read", "write"}},
		{Username: "bob", Email: "bob@example.com", FirstName: "Bob", LastName: "Jones",
			Enabled: true, Permissions: []string{"read"}},
		{Username: "charlie", Email: "charlie@example.com", FirstName: "Charlie", LastName: "Brown",
			Enabled: false, Permissions: []string{}},
	}
	now := time.Now()
	for i := range demo {
		demo[i].ID = newID()
		demo[i].CreatedAt = now
		demo[i].UpdatedAt = now
		s.users[demo[i].ID] = &demo[i]
	}
	s.persist()
}

// List returns all users sorted by created_at ascending, then id.
// A stable order is required for consistent pagination across requests.
func (s *Store) List() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Get returns the user with the given ID.
func (s *Store) Get(id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return *u, nil
}

// initAttributes returns a fresh attributes map seeded with the store defaults,
// then with any values from patch merged on top.
func (s *Store) initAttributes(patch map[string]any) map[string]any {
	if len(s.customFields) == 0 && len(patch) == 0 {
		return nil
	}
	attrs := make(map[string]any, len(s.customFields))
	for k, v := range s.customFields {
		attrs[k] = v
	}
	for k, v := range patch {
		attrs[k] = v
	}
	return attrs
}

// Create inserts a new user. Username must be unique.
func (s *Store) Create(u User) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.users {
		if existing.Username == u.Username {
			return User{}, ErrAlreadyExists
		}
	}
	now := time.Now()
	u.ID = newID()
	u.CreatedAt = now
	u.UpdatedAt = now
	if u.Permissions == nil {
		u.Permissions = []string{}
	}
	u.Attributes = s.initAttributes(u.Attributes)
	s.users[u.ID] = &u
	s.persist()
	return u, nil
}

// Update replaces editable fields of an existing user.
func (s *Store) Update(id string, patch User) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	if patch.Username != "" && patch.Username != u.Username {
		for _, existing := range s.users {
			if existing.ID != id && existing.Username == patch.Username {
				return User{}, ErrAlreadyExists
			}
		}
		u.Username = patch.Username
	}
	if patch.Email != "" {
		u.Email = patch.Email
	}
	if patch.FirstName != "" {
		u.FirstName = patch.FirstName
	}
	if patch.LastName != "" {
		u.LastName = patch.LastName
	}
	if patch.Manager != "" {
		u.Manager = patch.Manager
	}
	for k, v := range patch.Attributes {
		if u.Attributes == nil {
			u.Attributes = make(map[string]any)
		}
		u.Attributes[k] = v
	}
	u.UpdatedAt = time.Now()
	s.persist()
	return *u, nil
}

// SetEnabled enables or disables a user.
func (s *Store) SetEnabled(id string, enabled bool) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	u.Enabled = enabled
	u.UpdatedAt = time.Now()
	s.persist()
	return *u, nil
}

// SetManager sets (or clears) the manager field on a user.
func (s *Store) SetManager(id, managerID string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	u.Manager = managerID
	u.UpdatedAt = time.Now()
	s.persist()
	return *u, nil
}

// Delete removes a user.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return ErrNotFound
	}
	delete(s.users, id)
	s.persist()
	return nil
}

// GetPermissions returns the permission list for a user.
func (s *Store) GetPermissions(id string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]string, len(u.Permissions))
	copy(out, u.Permissions)
	return out, nil
}

// AddPermission appends a permission (idempotent).
func (s *Store) AddPermission(id, permission string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	for _, p := range u.Permissions {
		if p == permission {
			return append([]string{}, u.Permissions...), nil
		}
	}
	u.Permissions = append(u.Permissions, permission)
	u.UpdatedAt = time.Now()
	s.persist()
	return append([]string{}, u.Permissions...), nil
}

// RemovePermission removes a permission by value.
func (s *Store) RemovePermission(id, permission string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	updated := u.Permissions[:0]
	for _, p := range u.Permissions {
		if p != permission {
			updated = append(updated, p)
		}
	}
	u.Permissions = updated
	u.UpdatedAt = time.Now()
	s.persist()
	return append([]string{}, u.Permissions...), nil
}

// ---- helpers ----

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
