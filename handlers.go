package main

import (
	"encoding/csv"
	"encoding/xml"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ---- paged response types ----

// UserPage is the paginated list response for users.
type UserPage struct {
	XMLName    xml.Name `json:"-"          xml:"UserPage"`
	Users      []User   `json:"users"      xml:"User"`
	Pagination PageMeta `json:"pagination" xml:"Pagination"`
}

// ---- handler ----

// UserHandler wires REST endpoints to the Store.
type UserHandler struct {
	store      *Store
	pagination PaginationGroupConfig
}

func NewUserHandler(s *Store, pg PaginationGroupConfig) *UserHandler {
	return &UserHandler{store: s, pagination: pg}
}

// ---- LIST  GET /api/users ----

func (h *UserHandler) ListUsers(c *gin.Context) {
	page, pageSize, ok := parsePage(c, h.pagination)
	if !ok {
		return
	}
	all := h.store.List()
	slice, meta := paginate(all, page, pageSize)
	meta.NextPage = nextPageURL(c, page, meta.TotalPages)
	respond(c, http.StatusOK, UserPage{Users: slice, Pagination: meta})
}

// ---- EXPORT  GET /api/users/export ----

func (h *UserHandler) ExportUsers(c *gin.Context) {
	users := h.store.List()
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="users.csv"`)
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"id", "username", "email", "first_name", "last_name", "enabled", "permissions", "created_at", "updated_at"})
	for _, u := range users {
		_ = w.Write([]string{
			u.ID,
			u.Username,
			u.Email,
			u.FirstName,
			u.LastName,
			strconv.FormatBool(u.Enabled),
			strings.Join(u.Permissions, "; "),
			u.CreatedAt.Format("2006-01-02T15:04:05Z"),
			u.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	w.Flush()
}

// ---- GET  GET /api/users/:id ----

func (h *UserHandler) GetUser(c *gin.Context) {
	u, err := h.store.Get(c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, u)
}

// ---- CREATE  POST /api/users ----

func (h *UserHandler) CreateUser(c *gin.Context) {
	var u User
	if err := decodeBody(c, &u); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if u.Username == "" {
		respondMsg(c, http.StatusBadRequest, "username is required")
		return
	}
	created, err := h.store.Create(u)
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusCreated, created)
}

// ---- UPDATE  PUT /api/users/:id ----

func (h *UserHandler) UpdateUser(c *gin.Context) {
	var patch User
	if err := decodeBody(c, &patch); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	updated, err := h.store.Update(c.Param("id"), patch)
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, updated)
}

// ---- DELETE  DELETE /api/users/:id ----

func (h *UserHandler) DeleteUser(c *gin.Context) {
	if err := h.store.Delete(c.Param("id")); err != nil {
		respondError(c, err)
		return
	}
	respondMsg(c, http.StatusOK, "user deleted")
}

// ---- ENABLE / DISABLE ----

func (h *UserHandler) EnableUser(c *gin.Context) {
	u, err := h.store.SetEnabled(c.Param("id"), true)
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, u)
}

func (h *UserHandler) DisableUser(c *gin.Context) {
	u, err := h.store.SetEnabled(c.Param("id"), false)
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, u)
}

// ---- PERMISSIONS ----

type PermissionList struct {
	Permissions []string `json:"permissions" xml:"Permission"`
}

func (h *UserHandler) GetPermissions(c *gin.Context) {
	perms, err := h.store.GetPermissions(c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, PermissionList{Permissions: perms})
}

func (h *UserHandler) AddPermission(c *gin.Context) {
	var req struct {
		Permission string `json:"permission" xml:"Permission"`
	}
	if err := decodeBody(c, &req); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Permission == "" {
		respondMsg(c, http.StatusBadRequest, "permission is required")
		return
	}
	perms, err := h.store.AddPermission(c.Param("id"), req.Permission)
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, PermissionList{Permissions: perms})
}

func (h *UserHandler) RemovePermission(c *gin.Context) {
	perms, err := h.store.RemovePermission(c.Param("id"), c.Param("permission"))
	if err != nil {
		respondError(c, err)
		return
	}
	respond(c, http.StatusOK, PermissionList{Permissions: perms})
}

// ---- content-negotiation helpers ----

type apiMessage struct {
	Message string `json:"message" xml:"Message"`
}

func respond(c *gin.Context, status int, data any) {
	if wantsXML(c) {
		c.XML(status, data)
		return
	}
	c.IndentedJSON(status, data)
}

func respondMsg(c *gin.Context, status int, msg string) {
	respond(c, status, apiMessage{Message: msg})
}

func respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		respondMsg(c, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrAlreadyExists):
		respondMsg(c, http.StatusConflict, err.Error())
	default:
		respondMsg(c, http.StatusInternalServerError, err.Error())
	}
}

// wantsXML returns true when the client signals it wants XML.
// ?format=xml takes priority over the Accept header.
func wantsXML(c *gin.Context) bool {
	if f := c.Query("format"); f != "" {
		return strings.EqualFold(f, "xml")
	}
	accept := c.GetHeader("Accept")
	return strings.Contains(accept, "application/xml") || strings.Contains(accept, "text/xml")
}

// decodeBody decodes JSON or XML depending on Content-Type.
func decodeBody(c *gin.Context, v any) error {
	ct := c.ContentType()
	if strings.Contains(ct, "xml") {
		return xml.NewDecoder(c.Request.Body).Decode(v)
	}
	return c.ShouldBindJSON(v)
}
