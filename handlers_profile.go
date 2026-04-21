package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
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
	// field_map is internal→external, so look up "enabled" directly as a key.
	if route.Input != nil {
		if extKey, ok := route.Input.FieldMap["enabled"]; ok && extKey != "" {
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
	var userID, groupID string
	if urlGroupID := c.Param("groupId"); urlGroupID != "" {
		// Entra style: group in URL param, user read from request body.
		groupID = urlGroupID
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
		userID = extractUserID(rawID, route.UserBodyExtract)
	} else {
		// HelloID style: user in URL param :id, group read from request body.
		userID = c.Param("id")
		bodyField := route.UserBodyField
		if bodyField == "" {
			bodyField = "groupGuid"
		}
		var body map[string]any
		if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
			respondMsg(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		rawGroup, _ := body[bodyField].(string)
		if rawGroup == "" {
			respondMsg(c, http.StatusBadRequest, bodyField+" is required in request body")
			return
		}
		groupID = extractUserID(rawGroup, route.UserBodyExtract)
	}
	if _, err := h.store.AddPermission(userID, groupID); err != nil {
		respondError(c, err)
		return
	}
	c.Status(routeStatus(route, http.StatusOK))
}

func (h *ProfileHandler) actUnlinkGroup(c *gin.Context, route *Route) {
	userID := c.Param("userId")
	if userID == "" {
		userID = c.Param("id")
	}
	if _, err := h.store.RemovePermission(userID, c.Param("groupId")); err != nil {
		respondError(c, err)
		return
	}
	c.Status(routeStatus(route, http.StatusOK))
}

// ---- Person actions ----

func (h *ProfileHandler) actListPersons(c *gin.Context, route *Route) {
	out := route.Output
	persons := h.personStore.ListPersons()
	var items []any
	if out != nil && (out.MergeFirstContract != "" || out.MergeAllContracts != "") {
		items = make([]any, 0, len(persons))
		for _, p := range persons {
			merged, err := mergePersonContracts(p, h.personStore, out)
			if err != nil {
				respondMsg(c, http.StatusInternalServerError, err.Error())
				return
			}
			transformed, err := forwardTransform(merged, out)
			if err != nil {
				respondMsg(c, http.StatusInternalServerError, err.Error())
				return
			}
			items = append(items, transformed)
		}
	} else {
		var err error
		items, err = transformItems(persons, out)
		if err != nil {
			respondMsg(c, http.StatusInternalServerError, err.Error())
			return
		}
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
	var item map[string]any
	if route.Output != nil && (route.Output.MergeFirstContract != "" || route.Output.MergeAllContracts != "") {
		merged, merr := mergePersonContracts(p, h.personStore, route.Output)
		if merr != nil {
			respondMsg(c, http.StatusInternalServerError, merr.Error())
			return
		}
		item, err = forwardTransform(merged, route.Output)
	} else {
		item, err = forwardTransform(p, route.Output)
	}
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
	var resp map[string]any
	if len(route.Body) > 0 {
		_ = json.Unmarshal(route.Body, &resp)
	}
	if resp == nil {
		resp = make(map[string]any)
	}
	if route.TokenField != "" {
		setNestedValue(resp, route.TokenField, token)
	}
	c.IndentedJSON(routeStatus(route, http.StatusOK), resp)
}

// ---- Static action ----

func (h *ProfileHandler) actStatic(c *gin.Context, route *Route) {
	st := routeStatus(route, http.StatusOK)
	if len(route.Body) > 0 {
		c.Data(st, "application/json; charset=utf-8", route.Body)
		return
	}
	if noContent(c, st) {
		return
	}
	respondMsg(c, st, http.StatusText(st))
}

// ---- SOAP profile support ----

// parseSoapFields parses the direct child elements of the SOAP body root into a flat map.
func parseSoapFields(inner []byte) map[string]any {
	dec := xml.NewDecoder(bytes.NewReader(inner))
	result := make(map[string]any)
	depth := 0
	var currentKey string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				currentKey = t.Name.Local
			}
		case xml.CharData:
			if depth == 2 && currentKey != "" {
				if s := strings.TrimSpace(string(t)); s != "" {
					result[currentKey] = s
				}
			}
		case xml.EndElement:
			if depth == 2 {
				currentKey = ""
			}
			depth--
		}
	}
	return result
}

// soapGetField retrieves a value by internal key via input mapping, with common XML name fallbacks.
func soapGetField(fields map[string]any, in *InputMapping, internalKey string) string {
	if in != nil {
		if extKey, ok := in.FieldMap[internalKey]; ok {
			if v, ok := fields[extKey]; ok {
				return fmt.Sprintf("%v", v)
			}
		}
	}
	switch internalKey {
	case "id":
		if v, ok := fields["Id"]; ok {
			return fmt.Sprintf("%v", v)
		}
	case "person_id":
		if v, ok := fields["PersonId"]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// writeSOAPMap encodes a map[string]any as XML child elements recursively.
func writeSOAPMap(enc *xml.Encoder, m map[string]any) {
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			start := xml.StartElement{Name: xml.Name{Local: k}}
			_ = enc.EncodeToken(start)
			writeSOAPMap(enc, sub)
			_ = enc.EncodeToken(start.End())
		} else {
			_ = enc.EncodeElement(fmt.Sprintf("%v", v), xml.StartElement{Name: xml.Name{Local: k}})
		}
	}
}

func (h *ProfileHandler) soapRespond(c *gin.Context, responseName string, body func(*xml.Encoder)) {
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/xml; charset=utf-8")
	enc := xml.NewEncoder(c.Writer)
	enc.Indent("", "  ")
	_ = enc.EncodeToken(xml.StartElement{
		Name: xml.Name{Space: soapNS, Local: "Envelope"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "xmlns:soap"}, Value: soapNS},
			{Name: xml.Name{Local: "xmlns:tns"}, Value: apiNS},
		},
	})
	_ = enc.EncodeToken(xml.StartElement{Name: xml.Name{Space: soapNS, Local: "Body"}})
	_ = enc.EncodeToken(xml.StartElement{Name: xml.Name{Space: apiNS, Local: responseName}})
	body(enc)
	_ = enc.EncodeToken(xml.EndElement{Name: xml.Name{Space: apiNS, Local: responseName}})
	_ = enc.EncodeToken(xml.EndElement{Name: xml.Name{Space: soapNS, Local: "Body"}})
	_ = enc.EncodeToken(xml.EndElement{Name: xml.Name{Space: soapNS, Local: "Envelope"}})
	_ = enc.Flush()
}

func (h *ProfileHandler) soapFault(c *gin.Context, code, msg string) {
	c.Data(http.StatusBadRequest, "text/xml; charset=utf-8", []byte(fmt.Sprintf(
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<soap:Envelope xmlns:soap=%q>`+
			`<soap:Body><soap:Fault>`+
			`<faultcode>soap:%s</faultcode>`+
			`<faultstring>%s</faultstring>`+
			`</soap:Fault></soap:Body></soap:Envelope>`,
		soapNS, code, msg,
	)))
}

func (h *ProfileHandler) soapRespondOne(c *gin.Context, responseName string, internal any, out *OutputMapping) {
	item, err := forwardTransform(internal, out)
	if err != nil {
		h.soapFault(c, "Server", err.Error())
		return
	}
	h.soapRespond(c, responseName, func(enc *xml.Encoder) { writeSOAPMap(enc, item) })
}

func (h *ProfileHandler) soapRespondList(c *gin.Context, responseName string, items []any, out *OutputMapping) {
	itemKey := "Item"
	if out != nil && out.ItemKey != "" {
		itemKey = out.ItemKey
	}
	h.soapRespond(c, responseName, func(enc *xml.Encoder) {
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				start := xml.StartElement{Name: xml.Name{Local: itemKey}}
				_ = enc.EncodeToken(start)
				writeSOAPMap(enc, m)
				_ = enc.EncodeToken(start.End())
			}
		}
	})
}

func (h *ProfileHandler) soapRespondMsg(c *gin.Context, responseName, msg string) {
	h.soapRespond(c, responseName, func(enc *xml.Encoder) {
		_ = enc.EncodeElement(msg, xml.StartElement{Name: xml.Name{Local: "Message"}})
	})
}

// HandleSOAP dispatches a profile-defined SOAP operation using the route's action and mappings.
func (h *ProfileHandler) HandleSOAP(c *gin.Context, route *Route, inner []byte) {
	fields := parseSoapFields(inner)

	responseName := route.SoapResponse
	if responseName == "" {
		responseName = route.SoapOperation + "Response"
	}

	internalID := soapGetField(fields, route.Input, "id")

	switch route.Action {
	case "list_users":
		items, err := transformItems(h.store.List(), route.Output)
		if err != nil {
			h.soapFault(c, "Server", err.Error())
			return
		}
		h.soapRespondList(c, responseName, items, route.Output)

	case "get_user":
		u, err := h.store.Get(internalID)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, u, route.Output)

	case "create_user":
		var u User
		if _, err := reverseTransform(fields, route.Input, &u); err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		created, err := h.store.Create(u)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, created, route.Output)

	case "update_user":
		var patch User
		if _, err := reverseTransform(fields, route.Input, &patch); err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		updated, err := h.store.Update(internalID, patch)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, updated, route.Output)

	case "delete_user":
		if err := h.store.Delete(internalID); err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondMsg(c, responseName, "deleted")

	case "enable_user":
		u, err := h.store.SetEnabled(internalID, true)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, u, route.Output)

	case "disable_user":
		u, err := h.store.SetEnabled(internalID, false)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, u, route.Output)

	case "list_persons":
		items, err := transformItems(h.personStore.ListPersons(), route.Output)
		if err != nil {
			h.soapFault(c, "Server", err.Error())
			return
		}
		h.soapRespondList(c, responseName, items, route.Output)

	case "get_person":
		p, err := h.personStore.GetPerson(internalID)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, p, route.Output)

	case "create_person":
		var p Person
		if _, err := reverseTransform(fields, route.Input, &p); err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		created, err := h.personStore.CreatePerson(p)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, created, route.Output)

	case "update_person":
		var patch Person
		if _, err := reverseTransform(fields, route.Input, &patch); err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		updated, err := h.personStore.UpdatePerson(internalID, patch)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, updated, route.Output)

	case "delete_person":
		if err := h.personStore.DeletePerson(internalID); err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondMsg(c, responseName, "deleted")

	case "list_contracts":
		personID := soapGetField(fields, route.Input, "person_id")
		if personID == "" {
			personID = internalID
		}
		contracts, err := h.personStore.ListContracts(personID)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		items, err := transformItems(contracts, route.Output)
		if err != nil {
			h.soapFault(c, "Server", err.Error())
			return
		}
		h.soapRespondList(c, responseName, items, route.Output)

	case "get_contract":
		personID := soapGetField(fields, route.Input, "person_id")
		contractID := internalID
		ct, err := h.personStore.GetContract(personID, contractID)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, ct, route.Output)

	case "create_contract":
		personID := soapGetField(fields, route.Input, "person_id")
		if personID == "" {
			personID = internalID
		}
		var ct Contract
		if _, err := reverseTransform(fields, route.Input, &ct); err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		created, err := h.personStore.CreateContract(personID, ct)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, created, route.Output)

	case "update_contract":
		personID := soapGetField(fields, route.Input, "person_id")
		contractID := internalID
		var patch Contract
		if _, err := reverseTransform(fields, route.Input, &patch); err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		updated, err := h.personStore.UpdateContract(personID, contractID, patch)
		if err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondOne(c, responseName, updated, route.Output)

	case "delete_contract":
		personID := soapGetField(fields, route.Input, "person_id")
		contractID := internalID
		if err := h.personStore.DeleteContract(personID, contractID); err != nil {
			h.soapFault(c, "Client", err.Error())
			return
		}
		h.soapRespondMsg(c, responseName, "deleted")

	default:
		h.soapFault(c, "Client", "unsupported action for SOAP: "+route.Action)
	}
}

// RegisterProfileRoutes wires all profile-defined routes into the router.
func RegisterProfileRoutes(r *gin.Engine, p *Profile, auth *AuthService, userLimiter, personLimiter *ResourceLimiter, s *Store, ps *PersonStore, soap *SOAPHandler) {
	h := NewProfileHandler(p, s, ps, auth)
	soapRoutes := make(map[string]*Route)
	for i := range p.Routes {
		route := &p.Routes[i]
		if route.SoapOperation != "" {
			soapRoutes[route.SoapOperation] = route
			continue
		}
		var lim gin.HandlerFunc
		if route.Limiter == "person" {
			lim = personLimiter.Middleware()
		} else {
			lim = userLimiter.Middleware()
		}
		// issue_token is the auth endpoint itself — no token required to call it.
		if route.Action == "issue_token" {
			r.Handle(strings.ToUpper(route.Method), route.Path, func(c *gin.Context) {
				h.dispatch(c, route)
			})
		} else {
			r.Handle(strings.ToUpper(route.Method), route.Path, auth.Middleware(), lim, func(c *gin.Context) {
				h.dispatch(c, route)
			})
		}
	}
	if len(soapRoutes) > 0 {
		soap.SetProfileSOAP(h, soapRoutes)
	}
}
