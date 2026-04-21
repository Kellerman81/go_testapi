package main

import (
	"encoding/json"
	"fmt"
	"net/http"
"strings"

	"github.com/gin-gonic/gin"
)

// ProfileHandler handles requests for profile-mapped endpoints.
type ProfileHandler struct {
	profile     *Profile
	store       *Store
	personStore *PersonStore
	auth        *AuthService
}

func NewProfileHandler(p *Profile, s *Store, ps *PersonStore, a *AuthService) *ProfileHandler {
	return &ProfileHandler{profile: p, store: s, personStore: ps, auth: a}
}

// routeStatus returns route.Status if set, otherwise def.
func routeStatus(route *Route, def int) int {
	if route.Status != 0 {
		return route.Status
	}
	return def
}

// noContent writes a 204 and returns true. Returns false for any other status.
func noContent(c *gin.Context, status int) bool {
	if status == http.StatusNoContent {
		c.Status(status)
		return true
	}
	return false
}

// extractUserID extracts a user ID from a raw value using the configured extract strategy.
func extractUserID(raw, extract string) string {
	if extract == "last_path_segment" {
		raw = strings.TrimRight(raw, "/")
		if idx := strings.LastIndex(raw, "/"); idx >= 0 {
			return raw[idx+1:]
		}
	}
	return raw
}

// dispatch routes a request to the correct action handler.
func (h *ProfileHandler) dispatch(c *gin.Context, route *Route) {
	switch route.Action {
	case "list_users":
		h.actListUsers(c, route)
	case "get_user":
		h.actGetUser(c, route)
	case "create_user":
		h.actCreateUser(c, route)
	case "update_user":
		h.actUpdateUser(c, route)
	case "delete_user":
		h.actDeleteUser(c, route)
	case "enable_user":
		h.actEnableUser(c, route)
	case "disable_user":
		h.actDisableUser(c, route)
	case "list_groups":
		h.actListGroups(c, route)
	case "link_group":
		h.actLinkGroup(c, route)
	case "unlink_group":
		h.actUnlinkGroup(c, route)
	case "list_persons":
		h.actListPersons(c, route)
	case "get_person":
		h.actGetPerson(c, route)
	case "create_person":
		h.actCreatePerson(c, route)
	case "update_person":
		h.actUpdatePerson(c, route)
	case "delete_person":
		h.actDeletePerson(c, route)
	case "list_contracts":
		h.actListContracts(c, route)
	case "get_contract":
		h.actGetContract(c, route)
	case "create_contract":
		h.actCreateContract(c, route)
	case "update_contract":
		h.actUpdateContract(c, route)
	case "delete_contract":
		h.actDeleteContract(c, route)
	case "set_user_field":
		h.actSetUserField(c, route)
	case "clear_user_field":
		h.actClearUserField(c, route)
	case "issue_token":
		h.actIssueToken(c, route)
	case "static":
		h.actStatic(c, route)
	default:
		respondMsg(c, http.StatusNotImplemented, "unknown action: "+route.Action)
	}
}

// ---- User actions ----

func (h *ProfileHandler) actListUsers(c *gin.Context, route *Route) {
	out := route.Output
	all := h.store.List()
	items, err := transformItems(all, out)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	if out.Raw {
		c.IndentedJSON(http.StatusOK, items)
		return
	}
	c.IndentedJSON(http.StatusOK, wrapList(items, out))
}

func (h *ProfileHandler) actGetUser(c *gin.Context, route *Route) {
	id := c.Param("id")
	u, err := h.store.Get(id)
	if err != nil && route.LookupField != "" {
		matches := filterByEq(h.store.List(), route.LookupField, id)
		if len(matches) == 1 {
			u, err = matches[0], nil
		}
	}
	if err != nil {
		respondError(c, err)
		return
	}
	// Chain lookup: use an internal field on the user to fetch a second user (e.g. manager).
	if route.ChainField != "" {
		src, _ := structToMap(u)
		chainID, ok := getNestedValue(src, route.ChainField)
		if !ok {
			respondMsg(c, http.StatusNotFound, route.ChainField+" not set")
			return
		}
		manager, cerr := h.store.Get(fmt.Sprintf("%v", chainID))
		if cerr != nil {
			respondError(c, cerr)
			return
		}
		u = manager
	}
	item, err := forwardTransform(u, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(routeStatus(route, http.StatusOK), item)
}

func (h *ProfileHandler) actCreateUser(c *gin.Context, route *Route) {
	var body map[string]any
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	var u User
	if _, err := reverseTransform(body, route.Input, &u); err != nil {
		respondMsg(c, http.StatusBadRequest, err.Error())
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
	st := routeStatus(route, http.StatusCreated)
	if noContent(c, st) {
		return
	}
	item, err := forwardTransform(created, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

func (h *ProfileHandler) actUpdateUser(c *gin.Context, route *Route) {
	var body map[string]any
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	var patch User
	src, err := reverseTransform(body, route.Input, &patch)
	if err != nil {
		respondMsg(c, http.StatusBadRequest, err.Error())
		return
	}
	id := c.Param("id")
	updated, err := h.store.Update(id, patch)
	if err != nil {
		respondError(c, err)
		return
	}
	// Handle enabled specially: its zero value (false) is a valid update.
	if route.Input != nil {
		if extKey := reverseFieldLookup(route.Input.FieldMap, "enabled"); extKey != "" {
			if _, present := src[extKey]; present {
				updated, err = h.store.SetEnabled(id, patch.Enabled)
				if err != nil {
					respondError(c, err)
					return
				}
			}
		}
	}
	st := routeStatus(route, http.StatusOK)
	if noContent(c, st) {
		return
	}
	item, err := forwardTransform(updated, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

func (h *ProfileHandler) actDeleteUser(c *gin.Context, route *Route) {
	if err := h.store.Delete(c.Param("id")); err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusOK)
	if noContent(c, st) {
		return
	}
	respondMsg(c, st, "deleted")
}

func (h *ProfileHandler) actEnableUser(c *gin.Context, route *Route) {
	u, err := h.store.SetEnabled(c.Param("id"), true)
	if err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusOK)
	if route.Output == nil || noContent(c, st) {
		return
	}
	item, err := forwardTransform(u, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

func (h *ProfileHandler) actDisableUser(c *gin.Context, route *Route) {
	u, err := h.store.SetEnabled(c.Param("id"), false)
	if err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusOK)
	if route.Output == nil || noContent(c, st) {
		return
	}
	item, err := forwardTransform(u, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

// ---- Group actions ----

func (h *ProfileHandler) actListGroups(c *gin.Context, route *Route) {
	out := route.Output
	idField, nameField := "id", "name"
	if out != nil {
		if out.IDField != "" {
			idField = out.IDField
		}
		if out.NameField != "" {
			nameField = out.NameField
		}
	}
	seen := map[string]bool{}
	var groups []any
	for _, u := range h.store.List() {
		for _, perm := range u.Permissions {
			if seen[perm] {
				continue
			}
			seen[perm] = true
			groups = append(groups, map[string]any{idField: perm, nameField: perm})
		}
	}
	if groups == nil {
		groups = []any{}
	}
	if out == nil || out.Raw {
		c.IndentedJSON(http.StatusOK, groups)
		return
	}
	c.IndentedJSON(http.StatusOK, wrapList(groups, out))
}

func (h *ProfileHandler) actLinkGroup(c *gin.Context, route *Route) {
	groupID := c.Param("groupId")
	bodyField := route.UserBodyField
	if bodyField == "" {
		bodyField = "userId"
	}
	var body map[string]any
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	rawID, _ := body[bodyField].(string)
	if rawID == "" {
		respondMsg(c, http.StatusBadRequest, bodyField+" is required in request body")
		return
	}
	userID := extractUserID(rawID, route.UserBodyExtract)
	if _, err := h.store.AddPermission(userID, groupID); err != nil {
		respondError(c, err)
		return
	}
	c.Status(routeStatus(route, http.StatusOK))
}

func (h *ProfileHandler) actUnlinkGroup(c *gin.Context, route *Route) {
	if _, err := h.store.RemovePermission(c.Param("userId"), c.Param("groupId")); err != nil {
		respondError(c, err)
		return
	}
	c.Status(routeStatus(route, http.StatusOK))
}

// ---- Person actions ----

func (h *ProfileHandler) actListPersons(c *gin.Context, route *Route) {
	out := route.Output
	items, err := transformItems(h.personStore.ListPersons(), out)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	if out.Raw {
		c.IndentedJSON(http.StatusOK, items)
		return
	}
	c.IndentedJSON(http.StatusOK, wrapList(items, out))
}

func (h *ProfileHandler) actGetPerson(c *gin.Context, route *Route) {
	p, err := h.personStore.GetPerson(c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	item, err := forwardTransform(p, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(routeStatus(route, http.StatusOK), item)
}

func (h *ProfileHandler) actCreatePerson(c *gin.Context, route *Route) {
	var body map[string]any
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	var p Person
	if _, err := reverseTransform(body, route.Input, &p); err != nil {
		respondMsg(c, http.StatusBadRequest, err.Error())
		return
	}
	if p.FirstName == "" || p.LastName == "" {
		respondMsg(c, http.StatusBadRequest, "first_name and last_name are required")
		return
	}
	created, err := h.personStore.CreatePerson(p)
	if err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusCreated)
	if noContent(c, st) {
		return
	}
	item, err := forwardTransform(created, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

func (h *ProfileHandler) actUpdatePerson(c *gin.Context, route *Route) {
	var body map[string]any
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	var patch Person
	if _, err := reverseTransform(body, route.Input, &patch); err != nil {
		respondMsg(c, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.personStore.UpdatePerson(c.Param("id"), patch)
	if err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusOK)
	if noContent(c, st) {
		return
	}
	item, err := forwardTransform(updated, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

func (h *ProfileHandler) actDeletePerson(c *gin.Context, route *Route) {
	if err := h.personStore.DeletePerson(c.Param("id")); err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusOK)
	if noContent(c, st) {
		return
	}
	respondMsg(c, st, "deleted")
}

// ---- Contract actions ----

func (h *ProfileHandler) actListContracts(c *gin.Context, route *Route) {
	out := route.Output
	all, err := h.personStore.ListContracts(c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	items, err := transformItems(all, out)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	if out.Raw {
		c.IndentedJSON(http.StatusOK, items)
		return
	}
	c.IndentedJSON(http.StatusOK, wrapList(items, out))
}

func (h *ProfileHandler) actGetContract(c *gin.Context, route *Route) {
	ct, err := h.personStore.GetContract(c.Param("id"), c.Param("contractId"))
	if err != nil {
		respondError(c, err)
		return
	}
	item, err := forwardTransform(ct, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(routeStatus(route, http.StatusOK), item)
}

func (h *ProfileHandler) actCreateContract(c *gin.Context, route *Route) {
	var body map[string]any
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	var ct Contract
	if _, err := reverseTransform(body, route.Input, &ct); err != nil {
		respondMsg(c, http.StatusBadRequest, err.Error())
		return
	}
	if ct.Company == "" || ct.Title == "" || ct.StartDate == "" {
		respondMsg(c, http.StatusBadRequest, "company, title and start_date are required")
		return
	}
	created, err := h.personStore.CreateContract(c.Param("id"), ct)
	if err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusCreated)
	if noContent(c, st) {
		return
	}
	item, err := forwardTransform(created, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

func (h *ProfileHandler) actUpdateContract(c *gin.Context, route *Route) {
	var body map[string]any
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	var patch Contract
	if _, err := reverseTransform(body, route.Input, &patch); err != nil {
		respondMsg(c, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.personStore.UpdateContract(c.Param("id"), c.Param("contractId"), patch)
	if err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusOK)
	if noContent(c, st) {
		return
	}
	item, err := forwardTransform(updated, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

func (h *ProfileHandler) actDeleteContract(c *gin.Context, route *Route) {
	if err := h.personStore.DeleteContract(c.Param("id"), c.Param("contractId")); err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusOK)
	if noContent(c, st) {
		return
	}
	respondMsg(c, st, "deleted")
}

// actSetUserField reads a value from the request body and sets it on a user field.
// Configured via user_body_field, user_body_extract, and target_field.
func (h *ProfileHandler) actSetUserField(c *gin.Context, route *Route) {
	bodyField := route.UserBodyField
	if bodyField == "" {
		bodyField = "id"
	}
	var body map[string]any
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	rawVal, _ := body[bodyField].(string)
	value := extractUserID(rawVal, route.UserBodyExtract)
	u, err := h.dispatchSetField(c.Param("id"), route.TargetField, value)
	if err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusOK)
	if route.Output == nil || noContent(c, st) {
		return
	}
	item, err := forwardTransform(u, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

// actClearUserField sets a user field to its zero value.
func (h *ProfileHandler) actClearUserField(c *gin.Context, route *Route) {
	u, err := h.dispatchSetField(c.Param("id"), route.TargetField, "")
	if err != nil {
		respondError(c, err)
		return
	}
	st := routeStatus(route, http.StatusOK)
	if route.Output == nil || noContent(c, st) {
		return
	}
	item, err := forwardTransform(u, route.Output)
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.IndentedJSON(st, item)
}

// dispatchSetField routes a field-set operation to the correct store method.
func (h *ProfileHandler) dispatchSetField(id, field, value string) (User, error) {
	switch field {
	case "manager":
		return h.store.SetManager(id, value)
	default:
		return User{}, fmt.Errorf("set_user_field: unsupported target_field %q", field)
	}
}

// actIssueToken mints a real Bearer token and places it at route.TokenField in the response.
// The rest of the response body comes from route.Body (static fields).
func (h *ProfileHandler) actIssueToken(c *gin.Context, route *Route) {
	token, _ := h.auth.IssueToken()
	resp := make(map[string]any)
	for k, v := range route.Body {
		resp[k] = v
	}
	if route.TokenField != "" {
		setNestedValue(resp, route.TokenField, token)
	}
	c.IndentedJSON(routeStatus(route, http.StatusOK), resp)
}

// ---- Static action ----

func (h *ProfileHandler) actStatic(c *gin.Context, route *Route) {
	st := routeStatus(route, http.StatusOK)
	if route.Body != nil {
		c.IndentedJSON(st, route.Body)
		return
	}
	if noContent(c, st) {
		return
	}
	respondMsg(c, st, http.StatusText(st))
}

// RegisterProfileRoutes wires all profile-defined routes into the router.
func RegisterProfileRoutes(r *gin.Engine, p *Profile, auth *AuthService, userLimiter, personLimiter *ResourceLimiter, s *Store, ps *PersonStore) {
	h := NewProfileHandler(p, s, ps, auth)
	for i := range p.Routes {
		route := p.Routes[i]
		var lim gin.HandlerFunc
		if route.Limiter == "person" {
			lim = personLimiter.Middleware()
		} else {
			lim = userLimiter.Middleware()
		}
		// issue_token is the auth endpoint itself — no token required to call it.
		if route.Action == "issue_token" {
			r.Handle(strings.ToUpper(route.Method), route.Path, func(c *gin.Context) {
				h.dispatch(c, &route)
			})
		} else {
			r.Handle(strings.ToUpper(route.Method), route.Path, auth.Middleware(), lim, func(c *gin.Context) {
				h.dispatch(c, &route)
			})
		}
	}
}
