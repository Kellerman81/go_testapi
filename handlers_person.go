package main

import (
	"encoding/csv"
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ---- paged response types ----

// PersonPage is the paginated list response for persons.
type PersonPage struct {
	XMLName    xml.Name `json:"-"          xml:"PersonPage"`
	Persons    []Person `json:"persons"    xml:"Person"`
	Pagination PageMeta `json:"pagination" xml:"Pagination"`
}

// ContractPage is the paginated list response for contracts.
type ContractPage struct {
	XMLName    xml.Name   `json:"-"          xml:"ContractPage"`
	Contracts  []Contract `json:"contracts"  xml:"Contract"`
	Pagination PageMeta   `json:"pagination" xml:"Pagination"`
}

// ---- handler ----

// PersonHandler wires REST endpoints to the PersonStore.
type PersonHandler struct {
	store      *PersonStore
	pagination PaginationGroupConfig
}

func NewPersonHandler(s *PersonStore, pg PaginationGroupConfig) *PersonHandler {
	return &PersonHandler{store: s, pagination: pg}
}

// ---- Persons ----

// GET /api/persons
func (h *PersonHandler) ListPersons(c *gin.Context) {
	page, pageSize, ok := parsePage(c, h.pagination)
	if !ok {
		return
	}
	all := h.store.ListPersons()
	slice, meta := paginate(all, page, pageSize)
	meta.NextPage = nextPageURL(c, page, meta.TotalPages)
	respond(c, http.StatusOK, PersonPage{Persons: slice, Pagination: meta})
}

// GET /api/persons/:id
func (h *PersonHandler) GetPerson(c *gin.Context) {
	p, err := h.store.GetPerson(c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, p)
}

// POST /api/persons
func (h *PersonHandler) CreatePerson(c *gin.Context) {
	var p Person
	if err := decodeBody(c, &p); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if p.FirstName == "" || p.LastName == "" {
		respondMsg(c, http.StatusBadRequest, "first_name and last_name are required")
		return
	}
	created, err := h.store.CreatePerson(p)
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusCreated, created)
}

// PUT /api/persons/:id
func (h *PersonHandler) UpdatePerson(c *gin.Context) {
	var patch Person
	if err := decodeBody(c, &patch); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	updated, err := h.store.UpdatePerson(c.Param("id"), patch)
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, updated)
}

// DELETE /api/persons/:id
func (h *PersonHandler) DeletePerson(c *gin.Context) {
	if err := h.store.DeletePerson(c.Param("id")); err != nil {
		respondError(c, err)
		return
	}
	respondMsg(c, http.StatusOK, "person deleted")
}

// ---- EXPORT  GET /api/persons/export ----

func (h *PersonHandler) ExportPersons(c *gin.Context) {
	persons := h.store.ListPersons()
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="persons.csv"`)
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"id", "first_name", "last_name", "birthday", "street", "city", "state", "zip", "country", "phones", "created_at", "updated_at"})
	for _, p := range persons {
		_ = w.Write([]string{
			p.ID,
			p.FirstName,
			p.LastName,
			p.Birthday,
			p.Address.Street,
			p.Address.City,
			p.Address.State,
			p.Address.Zip,
			p.Address.Country,
			strings.Join(p.Phones, "; "),
			p.CreatedAt.Format("2006-01-02T15:04:05Z"),
			p.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	w.Flush()
}

// ---- EXPORT  GET /api/contracts/export ----

func (h *PersonHandler) ExportContracts(c *gin.Context) {
	contracts := h.store.ListAllContracts()
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="contracts.csv"`)
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"id", "person_id", "manager", "department", "company", "title", "start_date", "end_date", "created_at", "updated_at"})
	for _, ct := range contracts {
		_ = w.Write([]string{
			ct.ID,
			ct.PersonID,
			ct.Manager,
			ct.Department,
			ct.Company,
			ct.Title,
			ct.StartDate,
			ct.EndDate,
			ct.CreatedAt.Format("2006-01-02T15:04:05Z"),
			ct.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	w.Flush()
}

// ---- Contracts ----

// GET /api/contracts  — all contracts across every person
func (h *PersonHandler) ListAllContracts(c *gin.Context) {
	page, pageSize, ok := parsePage(c, h.pagination)
	if !ok {
		return
	}
	all := h.store.ListAllContracts()
	slice, meta := paginate(all, page, pageSize)
	meta.NextPage = nextPageURL(c, page, meta.TotalPages)
	respond(c, http.StatusOK, ContractPage{Contracts: slice, Pagination: meta})
}

// GET /api/persons/:id/contracts
func (h *PersonHandler) ListContracts(c *gin.Context) {
	page, pageSize, ok := parsePage(c, h.pagination)
	if !ok {
		return
	}
	all, err := h.store.ListContracts(c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	slice, meta := paginate(all, page, pageSize)
	meta.NextPage = nextPageURL(c, page, meta.TotalPages)
	respond(c, http.StatusOK, ContractPage{Contracts: slice, Pagination: meta})
}

// GET /api/persons/:id/contracts/:contractId
func (h *PersonHandler) GetContract(c *gin.Context) {
	contract, err := h.store.GetContract(c.Param("id"), c.Param("contractId"))
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, contract)
}

// POST /api/persons/:id/contracts
func (h *PersonHandler) CreateContract(c *gin.Context) {
	var ct Contract
	if err := decodeBody(c, &ct); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if ct.Company == "" || ct.Title == "" || ct.StartDate == "" {
		respondMsg(c, http.StatusBadRequest, "company, title and start_date are required")
		return
	}
	created, err := h.store.CreateContract(c.Param("id"), ct)
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusCreated, created)
}

// PUT /api/persons/:id/contracts/:contractId
func (h *PersonHandler) UpdateContract(c *gin.Context) {
	var patch Contract
	if err := decodeBody(c, &patch); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	updated, err := h.store.UpdateContract(c.Param("id"), c.Param("contractId"), patch)
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, updated)
}

// DELETE /api/persons/:id/contracts/:contractId
func (h *PersonHandler) DeleteContract(c *gin.Context) {
	if err := h.store.DeleteContract(c.Param("id"), c.Param("contractId")); err != nil {
		respondError(c, err)
		return
	}
	respondMsg(c, http.StatusOK, "contract deleted")
}
