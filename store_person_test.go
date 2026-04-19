package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestPersonStore returns a memory-backed person store with no seed data.
func newTestPersonStore() *PersonStore {
	return NewPersonStore("")
}

// makePerson is a test helper that creates a person and fatals on error.
func makePerson(t *testing.T, s *PersonStore, first, last string) Person {
	t.Helper()
	p, err := s.CreatePerson(Person{
		FirstName: first,
		LastName:  last,
		Birthday:  "1990-01-01",
		Address:   Address{Street: "1 Test St", City: "Testville", Country: "US"},
		Phones:    []string{"+1-555-0000"},
	})
	if err != nil {
		t.Fatalf("CreatePerson(%s %s): %v", first, last, err)
	}
	return p
}

// makeContract is a test helper that creates a contract and fatals on error.
func makeContract(t *testing.T, s *PersonStore, personID string) Contract {
	t.Helper()
	c, err := s.CreateContract(personID, Contract{
		Manager:    "Mgr",
		Department: "Dept",
		Company:    "Corp",
		Title:      "Engineer",
		StartDate:  "2024-01-01",
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	return c
}

// ---- Seed ----

func TestPersonStore_Seed_PopulatesData(t *testing.T) {
	s := newTestPersonStore()
	s.SeedPersons()
	if persons := s.ListPersons(); len(persons) == 0 {
		t.Fatal("expected seed data, got none")
	}
}

func TestPersonStore_Seed_SkipsWhenNotEmpty(t *testing.T) {
	s := newTestPersonStore()
	s.SeedPersons()
	first := len(s.ListPersons())
	s.SeedPersons()
	if got := len(s.ListPersons()); got != first {
		t.Fatalf("second SeedPersons() changed count: %d → %d", first, got)
	}
}

// ---- Person Create ----

func TestPersonStore_CreatePerson_OK(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Alice", "Smith")

	if p.ID == "" {
		t.Error("expected non-empty ID")
	}
	if p.FirstName != "Alice" || p.LastName != "Smith" {
		t.Errorf("name mismatch: got %q %q", p.FirstName, p.LastName)
	}
	if p.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if p.Phones == nil {
		t.Error("Phones should not be nil")
	}
}

func TestPersonStore_CreatePerson_NilPhonesBecomesEmpty(t *testing.T) {
	s := newTestPersonStore()
	p, err := s.CreatePerson(Person{FirstName: "Bob", LastName: "Jones"})
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	if p.Phones == nil {
		t.Error("Phones should be [] not nil after create")
	}
}

// ---- Person Get ----

func TestPersonStore_GetPerson_OK(t *testing.T) {
	s := newTestPersonStore()
	created := makePerson(t, s, "Carol", "White")
	got, err := s.GetPerson(created.ID)
	if err != nil {
		t.Fatalf("GetPerson: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, created.ID)
	}
}

func TestPersonStore_GetPerson_NotFound(t *testing.T) {
	s := newTestPersonStore()
	if _, err := s.GetPerson("nonexistent"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- Person Update ----

func TestPersonStore_UpdatePerson_Fields(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Dave", "Green")

	time.Sleep(20 * time.Millisecond)
	updated, err := s.UpdatePerson(p.ID, Person{
		FirstName: "David",
		LastName:  "Greene",
		Birthday:  "1985-06-15",
		Address:   Address{City: "NewCity"},
		Phones:    []string{"+1-555-9999"},
	})
	if err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}
	if updated.FirstName != "David" {
		t.Errorf("FirstName: got %q", updated.FirstName)
	}
	if updated.LastName != "Greene" {
		t.Errorf("LastName: got %q", updated.LastName)
	}
	if updated.Birthday != "1985-06-15" {
		t.Errorf("Birthday: got %q", updated.Birthday)
	}
	if updated.Address.City != "NewCity" {
		t.Errorf("Address.City: got %q", updated.Address.City)
	}
	if len(updated.Phones) != 1 || updated.Phones[0] != "+1-555-9999" {
		t.Errorf("Phones: got %v", updated.Phones)
	}
	if updated.UpdatedAt == p.UpdatedAt {
		t.Error("expected UpdatedAt to change")
	}
}

func TestPersonStore_UpdatePerson_EmptyFieldsPreserved(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Eve", "Black")

	// Patch only Birthday — name should be unchanged
	updated, err := s.UpdatePerson(p.ID, Person{Birthday: "2000-12-31"})
	if err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}
	if updated.FirstName != "Eve" {
		t.Errorf("FirstName changed unexpectedly: got %q", updated.FirstName)
	}
	if updated.Address.Street != p.Address.Street {
		t.Errorf("Address.Street changed unexpectedly")
	}
}

func TestPersonStore_UpdatePerson_NotFound(t *testing.T) {
	s := newTestPersonStore()
	if _, err := s.UpdatePerson("nonexistent", Person{FirstName: "X"}); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- Person Delete ----

func TestPersonStore_DeletePerson_OK(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Frank", "Gray")
	if err := s.DeletePerson(p.ID); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	if _, err := s.GetPerson(p.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPersonStore_DeletePerson_CascadesContracts(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Grace", "Tan")
	c := makeContract(t, s, p.ID)

	if err := s.DeletePerson(p.ID); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
	// Contracts for this person should be gone too
	if _, err := s.GetContract(p.ID, c.ID); err != ErrNotFound {
		t.Fatalf("expected contract to be cascade-deleted, got %v", err)
	}
}

func TestPersonStore_DeletePerson_NotFound(t *testing.T) {
	s := newTestPersonStore()
	if err := s.DeletePerson("nonexistent"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- Contract Create ----

func TestPersonStore_CreateContract_OK(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Hank", "Blue")
	c := makeContract(t, s, p.ID)

	if c.ID == "" {
		t.Error("expected non-empty contract ID")
	}
	if c.PersonID != p.ID {
		t.Errorf("PersonID: got %q, want %q", c.PersonID, p.ID)
	}
	if c.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestPersonStore_CreateContract_PersonNotFound(t *testing.T) {
	s := newTestPersonStore()
	if _, err := s.CreateContract("nonexistent", Contract{Company: "X", Title: "Y", StartDate: "2024-01-01"}); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- Contract Get / List ----

func TestPersonStore_ListContracts_OK(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Iris", "Red")
	makeContract(t, s, p.ID)
	makeContract(t, s, p.ID)

	contracts, err := s.ListContracts(p.ID)
	if err != nil {
		t.Fatalf("ListContracts: %v", err)
	}
	if len(contracts) != 2 {
		t.Fatalf("expected 2 contracts, got %d", len(contracts))
	}
}

func TestPersonStore_ListContracts_EmptySliceNotNil(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Jack", "Pink")
	contracts, err := s.ListContracts(p.ID)
	if err != nil {
		t.Fatalf("ListContracts: %v", err)
	}
	if contracts == nil {
		t.Error("expected empty slice, got nil")
	}
}

func TestPersonStore_GetContract_WrongPerson(t *testing.T) {
	s := newTestPersonStore()
	p1 := makePerson(t, s, "Karl", "Gold")
	p2 := makePerson(t, s, "Lena", "Silver")
	c := makeContract(t, s, p1.ID)

	// Querying p2's contracts for p1's contract ID should return not found.
	if _, err := s.GetContract(p2.ID, c.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for wrong person, got %v", err)
	}
}

// ---- Contract Update ----

func TestPersonStore_UpdateContract_Fields(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Mia", "Brown")
	c := makeContract(t, s, p.ID)

	time.Sleep(20 * time.Millisecond)
	updated, err := s.UpdateContract(p.ID, c.ID, Contract{
		Manager:    "NewMgr",
		Department: "NewDept",
		Company:    "NewCorp",
		Title:      "Lead",
		StartDate:  "2025-03-01",
		EndDate:    "2026-03-01",
	})
	if err != nil {
		t.Fatalf("UpdateContract: %v", err)
	}
	if updated.Manager != "NewMgr" {
		t.Errorf("Manager: got %q", updated.Manager)
	}
	if updated.Title != "Lead" {
		t.Errorf("Title: got %q", updated.Title)
	}
	if updated.EndDate != "2026-03-01" {
		t.Errorf("EndDate: got %q", updated.EndDate)
	}
	if updated.UpdatedAt == c.UpdatedAt {
		t.Error("expected UpdatedAt to change")
	}
}

func TestPersonStore_UpdateContract_ClearEndDate(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Nick", "Violet")
	c, _ := s.CreateContract(p.ID, Contract{Company: "X", Title: "Y", StartDate: "2024-01-01", EndDate: "2025-01-01"})

	updated, err := s.UpdateContract(p.ID, c.ID, Contract{EndDate: ""})
	if err != nil {
		t.Fatalf("UpdateContract: %v", err)
	}
	if updated.EndDate != "" {
		t.Errorf("expected EndDate to be cleared, got %q", updated.EndDate)
	}
}

func TestPersonStore_UpdateContract_NotFound(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Olivia", "Cyan")
	if _, err := s.UpdateContract(p.ID, "nonexistent", Contract{Title: "X"}); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- Contract Delete ----

func TestPersonStore_DeleteContract_OK(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Paul", "Mauve")
	c := makeContract(t, s, p.ID)

	if err := s.DeleteContract(p.ID, c.ID); err != nil {
		t.Fatalf("DeleteContract: %v", err)
	}
	if _, err := s.GetContract(p.ID, c.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPersonStore_DeleteContract_NotFound(t *testing.T) {
	s := newTestPersonStore()
	p := makePerson(t, s, "Quinn", "Amber")
	if err := s.DeleteContract(p.ID, "nonexistent"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---- File persistence ----

func TestPersonStore_FilePersistence_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	store1 := NewPersonStore(dir)
	p, _ := store1.CreatePerson(Person{
		FirstName: "Rita",
		LastName:  "Blue",
		Birthday:  "1988-04-10",
		Address:   Address{City: "Paris", Country: "FR"},
		Phones:    []string{"+33-1-0000"},
	})
	c, _ := store1.CreateContract(p.ID, Contract{
		Company: "ACME", Title: "Analyst", StartDate: "2023-01-01",
	})

	// Verify file written
	if _, err := os.Stat(filepath.Join(dir, "persons.json")); err != nil {
		t.Fatalf("persons.json not created: %v", err)
	}

	// Reload from disk
	store2 := NewPersonStore(dir)

	loadedP, err := store2.GetPerson(p.ID)
	if err != nil {
		t.Fatalf("GetPerson after reload: %v", err)
	}
	if loadedP.FirstName != "Rita" {
		t.Errorf("FirstName: got %q", loadedP.FirstName)
	}
	if loadedP.Address.City != "Paris" {
		t.Errorf("Address.City: got %q", loadedP.Address.City)
	}

	loadedC, err := store2.GetContract(p.ID, c.ID)
	if err != nil {
		t.Fatalf("GetContract after reload: %v", err)
	}
	if loadedC.Company != "ACME" {
		t.Errorf("Company: got %q", loadedC.Company)
	}
}

func TestPersonStore_FilePersistence_DeleteSurvidesReload(t *testing.T) {
	dir := t.TempDir()

	store1 := NewPersonStore(dir)
	p := makePerson(t, store1, "Sam", "Orange")
	store1.DeletePerson(p.ID)

	store2 := NewPersonStore(dir)
	if _, err := store2.GetPerson(p.ID); err != ErrNotFound {
		t.Fatalf("deleted person reappeared after reload: %v", err)
	}
}

func TestPersonStore_FilePersistence_SeedSkippedWhenDataExists(t *testing.T) {
	dir := t.TempDir()

	store1 := NewPersonStore(dir)
	store1.CreatePerson(Person{FirstName: "Existing", LastName: "Person"})

	store2 := NewPersonStore(dir)
	store2.SeedPersons() // should be a no-op
	if got := len(store2.ListPersons()); got != 1 {
		t.Errorf("expected 1 person (not seeded), got %d", got)
	}
}
