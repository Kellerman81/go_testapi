package main

import (
	"maps"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ---- domain models ----

type Address struct {
	Street  string `json:"street"  xml:"Street"`
	City    string `json:"city"    xml:"City"`
	State   string `json:"state"   xml:"State"`
	Zip     string `json:"zip"     xml:"Zip"`
	Country string `json:"country" xml:"Country"`
}

type Person struct {
	ID         string         `json:"id"                   xml:"Id"`
	FirstName  string         `json:"first_name"           xml:"FirstName"`
	LastName   string         `json:"last_name"            xml:"LastName"`
	Email      string         `json:"email"                xml:"Email"`
	Birthday   string         `json:"birthday"             xml:"Birthday"` // YYYY-MM-DD
	Address    Address        `json:"address"              xml:"Address"`
	Phones     []string       `json:"phones"               xml:"Phones>Phone,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty" xml:"-"`
	CreatedAt  time.Time      `json:"created_at"           xml:"CreatedAt"`
	UpdatedAt  time.Time      `json:"updated_at"           xml:"UpdatedAt"`
}

// PersonList wraps a slice for XML serialisation.
type PersonList struct {
	Persons []Person `json:"persons" xml:"Person"`
}

type Contract struct {
	ID         string         `json:"id"                   xml:"Id"`
	PersonID   string         `json:"person_id"            xml:"PersonId"`
	Manager    string         `json:"manager"              xml:"Manager"`
	Department string         `json:"department"           xml:"Department"`
	Company    string         `json:"company"              xml:"Company"`
	Title      string         `json:"title"                xml:"Title"`
	StartDate  string         `json:"start_date"           xml:"StartDate"` // YYYY-MM-DD
	EndDate    string         `json:"end_date"             xml:"EndDate"`   // YYYY-MM-DD, empty = current
	Attributes map[string]any `json:"attributes,omitempty" xml:"-"`
	CreatedAt  time.Time      `json:"created_at"           xml:"CreatedAt"`
	UpdatedAt  time.Time      `json:"updated_at"           xml:"UpdatedAt"`
}

// ContractList wraps a slice for XML serialisation.
type ContractList struct {
	Contracts []Contract `json:"contracts" xml:"Contract"`
}

// ---- on-disk representation ----

// personFile is the JSON structure written to persons.json.
// Keeping contracts inside the file with their person_id FK makes the file
// self-contained and human-readable.
type personFile struct {
	Persons   []Person   `json:"persons"`
	Contracts []Contract `json:"contracts"`
}

// ---- store ----

// PersonStore is a thread-safe store for persons and their contracts,
// with optional file persistence.
type PersonStore struct {
	mu             sync.RWMutex
	persons        map[string]*Person
	contracts      map[string]*Contract // keyed by contract ID
	dataDir        string               // empty = memory-only
	personFields   map[string]any       // default attributes for new persons
	contractFields map[string]any       // default attributes for new contracts
}

// NewPersonStore returns a store. When dataDir is non-empty it loads
// existing data from disk and persists every mutation.
// personFields / contractFields define attribute keys and defaults for each entity type.
func NewPersonStore(dataDir string, personFields, contractFields map[string]any) *PersonStore {
	s := &PersonStore{
		persons:        make(map[string]*Person),
		contracts:      make(map[string]*Contract),
		dataDir:        dataDir,
		personFields:   personFields,
		contractFields: contractFields,
	}
	if dataDir != "" {
		var pf personFile
		if err := loadJSON(s.filePath(), &pf); err != nil {
			logPersistErr("persons load", err)
		}
		for i := range pf.Persons {
			p := pf.Persons[i]
			if p.Phones == nil {
				p.Phones = []string{}
			}
			s.persons[p.ID] = &p
		}
		for i := range pf.Contracts {
			c := pf.Contracts[i]
			s.contracts[c.ID] = &c
		}
	}
	return s
}

func (s *PersonStore) filePath() string {
	return filepath.Join(s.dataDir, "persons.json")
}

// persist writes persons + contracts to disk.
// Must be called while s.mu write lock is held.
func (s *PersonStore) persist() {
	if s.dataDir == "" {
		return
	}
	pf := personFile{
		Persons:   make([]Person, 0, len(s.persons)),
		Contracts: make([]Contract, 0, len(s.contracts)),
	}
	for _, p := range s.persons {
		pf.Persons = append(pf.Persons, *p)
	}
	for _, c := range s.contracts {
		pf.Contracts = append(pf.Contracts, *c)
	}
	logPersistErr("persons save", saveJSON(s.filePath(), pf))
}

// SeedPersons populates demo data only when the store is empty.
func (s *PersonStore) SeedPersons() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.persons) > 0 {
		return // already has data (loaded from disk)
	}
	now := time.Now()
	people := []struct {
		p Person
		c []Contract
	}{
		{
			Person{
				FirstName: "John", LastName: "Doe", Email: "john.doe@example.com", Birthday: "1985-03-12",
				Address: Address{Street: "123 Main St", City: "Springfield", State: "IL", Zip: "62701", Country: "US"},
				Phones:  []string{"+1-555-0100", "+1-555-0101"},
			},
			[]Contract{
				{Manager: "Jane Smith", Department: "Engineering", Company: "Acme Corp", Title: "Senior Developer", StartDate: "2020-01-15"},
			},
		},
		{
			Person{
				FirstName: "Mary", LastName: "Johnson", Email: "mary.johnson@example.com", Birthday: "1990-07-24",
				Address: Address{Street: "45 Oak Ave", City: "Shelbyville", State: "IL", Zip: "62565", Country: "US"},
				Phones:  []string{"+1-555-0200"},
			},
			[]Contract{
				{Manager: "Bob Brown", Department: "Marketing", Company: "Globex", Title: "Marketing Lead", StartDate: "2019-06-01", EndDate: "2022-12-31"},
				{Manager: "Alice Green", Department: "Sales", Company: "Globex", Title: "Sales Manager", StartDate: "2023-01-01"},
			},
		},
	}
	for i := range people {
		p := &people[i].p
		p.ID = newID()
		p.CreatedAt = now
		p.UpdatedAt = now
		s.persons[p.ID] = p
		for j := range people[i].c {
			c := &people[i].c[j]
			c.ID = newID()
			c.PersonID = p.ID
			c.CreatedAt = now
			c.UpdatedAt = now
			s.contracts[c.ID] = c
		}
	}
	s.persist()
}

// ---- Person CRUD ----

func (s *PersonStore) ListPersons() []Person {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Person, 0, len(s.persons))
	for _, p := range s.persons {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// ---- sort helpers ----

func sortContracts(out []Contract) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
}

func (s *PersonStore) GetPerson(id string) (Person, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.persons[id]
	if !ok {
		return Person{}, ErrNotFound
	}
	return *p, nil
}

// initAttrs builds an attributes map seeded with defaults then patched with provided values.
func initAttrs(defaults, patch map[string]any) map[string]any {
	if len(defaults) == 0 && len(patch) == 0 {
		return nil
	}
	attrs := make(map[string]any, len(defaults))
	maps.Copy(attrs, defaults)
	maps.Copy(attrs, patch)
	return attrs
}

func (s *PersonStore) CreatePerson(p Person) (Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	p.ID = newID()
	p.CreatedAt = now
	p.UpdatedAt = now
	if p.Phones == nil {
		p.Phones = []string{}
	}
	p.Attributes = initAttrs(s.personFields, p.Attributes)
	s.persons[p.ID] = &p
	s.persist()
	return p, nil
}

func (s *PersonStore) UpdatePerson(id string, patch Person) (Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.persons[id]
	if !ok {
		return Person{}, ErrNotFound
	}
	if patch.FirstName != "" {
		p.FirstName = patch.FirstName
	}
	if patch.LastName != "" {
		p.LastName = patch.LastName
	}
	if patch.Email != "" {
		p.Email = patch.Email
	}
	if patch.Birthday != "" {
		p.Birthday = patch.Birthday
	}
	if patch.Address != (Address{}) {
		if patch.Address.Street != "" {
			p.Address.Street = patch.Address.Street
		}
		if patch.Address.City != "" {
			p.Address.City = patch.Address.City
		}
		if patch.Address.State != "" {
			p.Address.State = patch.Address.State
		}
		if patch.Address.Zip != "" {
			p.Address.Zip = patch.Address.Zip
		}
		if patch.Address.Country != "" {
			p.Address.Country = patch.Address.Country
		}
	}
	if patch.Phones != nil {
		p.Phones = patch.Phones
	}
	for k, v := range patch.Attributes {
		if p.Attributes == nil {
			p.Attributes = make(map[string]any)
		}
		p.Attributes[k] = v
	}
	p.UpdatedAt = time.Now()
	s.persist()
	return *p, nil
}

func (s *PersonStore) DeletePerson(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.persons[id]; !ok {
		return ErrNotFound
	}
	delete(s.persons, id)
	for cid, c := range s.contracts {
		if c.PersonID == id {
			delete(s.contracts, cid)
		}
	}
	s.persist()
	return nil
}

// ---- Contract CRUD ----

// ListAllContracts returns every contract across all persons, sorted by
// created_at ascending then id for stable pagination.
func (s *PersonStore) ListAllContracts() []Contract {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Contract, 0, len(s.contracts))
	for _, c := range s.contracts {
		out = append(out, *c)
	}
	sortContracts(out)
	return out
}

func (s *PersonStore) ListContracts(personID string) ([]Contract, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.persons[personID]; !ok {
		return nil, ErrNotFound
	}
	var out []Contract
	for _, c := range s.contracts {
		if c.PersonID == personID {
			out = append(out, *c)
		}
	}
	if out == nil {
		out = []Contract{}
	}
	sortContracts(out)
	return out, nil
}

func (s *PersonStore) GetContract(personID, contractID string) (Contract, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.persons[personID]; !ok {
		return Contract{}, ErrNotFound
	}
	c, ok := s.contracts[contractID]
	if !ok || c.PersonID != personID {
		return Contract{}, ErrNotFound
	}
	return *c, nil
}

func (s *PersonStore) CreateContract(personID string, c Contract) (Contract, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.persons[personID]; !ok {
		return Contract{}, ErrNotFound
	}
	now := time.Now()
	c.ID = newID()
	c.PersonID = personID
	c.CreatedAt = now
	c.UpdatedAt = now
	c.Attributes = initAttrs(s.contractFields, c.Attributes)
	s.contracts[c.ID] = &c
	s.persist()
	return c, nil
}

func (s *PersonStore) UpdateContract(personID, contractID string, patch Contract) (Contract, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.persons[personID]; !ok {
		return Contract{}, ErrNotFound
	}
	c, ok := s.contracts[contractID]
	if !ok || c.PersonID != personID {
		return Contract{}, ErrNotFound
	}
	if patch.Manager != "" {
		c.Manager = patch.Manager
	}
	if patch.Department != "" {
		c.Department = patch.Department
	}
	if patch.Company != "" {
		c.Company = patch.Company
	}
	if patch.Title != "" {
		c.Title = patch.Title
	}
	if patch.StartDate != "" {
		c.StartDate = patch.StartDate
	}
	// EndDate can be explicitly cleared by sending "" — always copy it
	c.EndDate = patch.EndDate
	for k, v := range patch.Attributes {
		if c.Attributes == nil {
			c.Attributes = make(map[string]any)
		}
		c.Attributes[k] = v
	}
	c.UpdatedAt = time.Now()
	s.persist()
	return *c, nil
}

func (s *PersonStore) DeleteContract(personID, contractID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.persons[personID]; !ok {
		return ErrNotFound
	}
	c, ok := s.contracts[contractID]
	if !ok || c.PersonID != personID {
		return ErrNotFound
	}
	delete(s.contracts, contractID)
	s.persist()
	return nil
}
