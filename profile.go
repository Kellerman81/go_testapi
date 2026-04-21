package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"
)

// InputMapping defines how an external request body is decoded into an internal struct.
type InputMapping struct {
	FieldMap       map[string]string `json:"field_map"`       // external key → internal key
	ItemKey        string            `json:"item_key"`        // unwrap body from this key first
	AttributeStyle string            `json:"attribute_style"` // "" or "labeled"
}

// OutputMapping defines how an internal struct is shaped into an external response.
type OutputMapping struct {
	FieldMap map[string]string `json:"field_map"` // internal key → external key

	// Per-item shaping
	ItemKey   string         `json:"item_key"`   // nest mapped fields under this key
	ItemExtra map[string]any `json:"item_extra"` // merged alongside ItemKey in each item

	// List-envelope shaping (ignored when Raw is true)
	ListKey   string         `json:"list_key"`   // wrap array under this key (default "items")
	ListExtra map[string]any `json:"list_extra"` // merged into the list wrapper object
	Raw       bool           `json:"raw"`        // return a plain JSON array

	// Group object field names
	IDField   string `json:"id_field"`
	NameField string `json:"name_field"`

	AttributeStyle string `json:"attribute_style"` // "" or "labeled"

	// FieldFormats maps external field names to a date format pattern applied on output.
	// Tokens: yyyy MM dd HH mm ss Z (timezone as ±HH:mm)
	// Example: "yyyy-MM-dd'T'HH:mm:ssZ" → "2014-11-09T00:00:00+00:00"
	//          "dd.MM.yyyy"             → "09.11.2014"
	FieldFormats map[string]string `json:"field_formats"`
}

// Route maps one external HTTP endpoint to one internal action.
type Route struct {
	Method string `json:"method"` // HTTP method (GET, POST, PUT, PATCH, DELETE)
	Path   string `json:"path"`   // URL path; use :id, :groupId, :userId, :contractId as params
	Action string `json:"action"` // internal action name (see dispatch)
	Status int    `json:"status"` // response status override (0 = action default)

	// Get options
	LookupField string `json:"lookup_field"` // internal field to search when :id lookup fails
	ChainField  string `json:"chain_field"`  // internal field whose value is looked up as a second user (e.g. manager)

	// TargetField is the internal User field name used by set_user_field / clear_user_field actions.
	TargetField string `json:"target_field"`

	// SoapOperation: when set, this route handles a SOAP operation at the /soap endpoint.
	// method and path are ignored. The operation is matched by the SOAP body root element name.
	SoapOperation string `json:"soap_operation"`
	// SoapResponse is the response wrapper element name. Defaults to SoapOperation+"Response".
	SoapResponse string `json:"soap_response"`

	// TokenField is the dot-notation path in the response where the issued token is placed.
	// Used by the issue_token action. The rest of the response comes from body.
	// Example: "data.token" → { ..., "data": { "token": "<token>" } }
	TokenField string `json:"token_field"`

	// Group membership options
	UserBodyField   string `json:"user_body_field"`   // JSON field in POST body that holds the user ID
	UserBodyExtract string `json:"user_body_extract"` // "" (direct) or "last_path_segment"

	Input  *InputMapping    `json:"input"`  // request body mapping (create/update)
	Output *OutputMapping   `json:"output"` // response shaping (list/get/create/update)
	Body   json.RawMessage  `json:"body"`   // static response body; any JSON value (object, array, …)

	// Limiter selects the rate-limiter bucket: "user" (default) or "person"
	Limiter string `json:"limiter"`
}

// Profile defines a named API mock as a flat list of individually-mapped routes.
type Profile struct {
	Name   string  `json:"name"`
	Routes []Route `json:"routes"`
}

// LoadProfile reads a profile JSON file. Returns nil without error when path is empty.
func LoadProfile(path string) (*Profile, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var p Profile
	if err := json.NewDecoder(f).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// structToMap converts a struct to map[string]any via JSON, preserving nested objects.
func structToMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

// mapToStruct converts a map[string]any to a struct via JSON.
func mapToStruct(m map[string]any, v any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// getNestedValue retrieves a value from a nested map using a dot-notation path.
func getNestedValue(m map[string]any, path string) (any, bool) {
	parts := strings.SplitN(path, ".", 2)
	val, ok := m[parts[0]]
	if !ok || len(parts) == 1 {
		return val, ok
	}
	nested, ok := val.(map[string]any)
	if !ok {
		return nil, false
	}
	return getNestedValue(nested, parts[1])
}

// setNestedValue sets a value in a nested map using a dot-notation path,
// creating intermediate maps as needed.
func setNestedValue(m map[string]any, path string, val any) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) == 1 {
		m[path] = val
		return
	}
	sub, _ := m[parts[0]].(map[string]any)
	if sub == nil {
		sub = make(map[string]any)
		m[parts[0]] = sub
	}
	setNestedValue(sub, parts[1], val)
}

// attributeLabel derives a human-readable label from a snake_case field name.
func attributeLabel(name string) string {
	words := strings.Split(name, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// attributeType guesses a type tag from a Go value ("integer", "date", "standard").
func attributeType(val any) string {
	switch val.(type) {
	case float64, int, int64:
		return "integer"
	default:
		if s, ok := val.(string); ok && len(s) == 10 && s[4] == '-' && s[7] == '-' {
			return "date"
		}
		return "standard"
	}
}

// patternToGoFormat converts a dd.MM.yyyy-style pattern to a Go time layout string.
func patternToGoFormat(pattern string) string {
	r := strings.NewReplacer(
		"yyyy", "2006",
		"MM",   "01",
		"dd",   "02",
		"HH",   "15",
		"mm",   "04",
		"ss",   "05",
		"Z",    "-07:00",
	)
	// Strip optional single-quoted literals (e.g. 'T') used to escape separators.
	out := r.Replace(pattern)
	out = strings.ReplaceAll(out, "'", "")
	return out
}

// applyFieldFormat applies a named conversion or date pattern to a field value.
// Reserved formats: "int", "float", "string".
// Anything else is treated as a date pattern (yyyy MM dd HH mm ss Z tokens).
func applyFieldFormat(val any, pattern string) any {
	switch pattern {
	case "int":
		switch v := val.(type) {
		case float64:
			return int64(v)
		case string:
			var i int64
			if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
				return i
			}
		}
		return val
	case "float":
		switch v := val.(type) {
		case int64, int:
			return fmt.Sprintf("%v", v)
		case string:
			var f float64
			if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
				return f
			}
		}
		return val
	case "string":
		return fmt.Sprintf("%v", val)
	default:
		s, ok := val.(string)
		if !ok || s == "" {
			return val
		}
		goFmt := patternToGoFormat(pattern)
		for _, layout := range []string{
			"2006-01-02T15:04:05Z07:00",
			"2006-01-02T15:04:05Z",
			"2006-01-02",
		} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.Format(goFmt)
			}
		}
		return val
	}
}

// forwardTransform maps an internal struct to an OutputMapping-shaped response item.
func forwardTransform(internal any, out *OutputMapping) (map[string]any, error) {
	src, err := structToMap(internal)
	if err != nil {
		return nil, err
	}

	fields := make(map[string]any, len(out.FieldMap))
	for internalKey, externalKey := range out.FieldMap {
		val, ok := getNestedValue(src, internalKey)
		if !ok {
			continue
		}
		if fmt, ok := out.FieldFormats[externalKey]; ok {
			val = applyFieldFormat(val, fmt)
		}
		if out.AttributeStyle == "labeled" {
			fields[externalKey] = map[string]any{
				"label": attributeLabel(externalKey),
				"value": val,
				"type":  attributeType(val),
			}
		} else {
			fields[externalKey] = val
		}
	}

	item := make(map[string]any, len(out.ItemExtra)+2)
	maps.Copy(item, out.ItemExtra)
	if out.ItemKey != "" {
		item[out.ItemKey] = fields
	} else {
		maps.Copy(item, fields)
	}
	return item, nil
}

// reverseTransform maps an external (InputMapping-shaped) request body to an internal struct.
// Accepts both labeled {"label","value","type"} and plain values when attribute_style is "labeled".
// Returns the raw source map (after unwrapping item_key) so callers can detect which fields
// were present in the body.
func reverseTransform(body map[string]any, in *InputMapping, dest any) (map[string]any, error) {
	src := body
	fieldMap := map[string]string{}
	attributeStyle := ""
	if in != nil {
		fieldMap = in.FieldMap
		attributeStyle = in.AttributeStyle
		if in.ItemKey != "" {
			if nested, ok := body[in.ItemKey].(map[string]any); ok {
				src = nested
			}
		}
	}

	internal := make(map[string]any, len(fieldMap))
	for internalKey, externalKey := range fieldMap {
		raw, ok := src[externalKey]
		if !ok {
			continue
		}
		var val any
		if attributeStyle == "labeled" {
			if wrapper, wok := raw.(map[string]any); wok {
				val = wrapper["value"]
			} else {
				val = raw
			}
		} else {
			val = raw
		}
		setNestedValue(internal, internalKey, val)
	}
	return src, mapToStruct(internal, dest)
}

// wrapList wraps a slice of items in the configured list envelope.
func wrapList(items []any, out *OutputMapping) map[string]any {
	key := out.ListKey
	if key == "" {
		key = "items"
	}
	result := make(map[string]any, len(out.ListExtra)+1)
	maps.Copy(result, out.ListExtra)
	result[key] = items
	return result
}

// transformItems applies forwardTransform to every element of a typed slice.
func transformItems[T any](items []T, out *OutputMapping) ([]any, error) {
	result := make([]any, len(items))
	for i, item := range items {
		transformed, err := forwardTransform(item, out)
		if err != nil {
			return nil, err
		}
		result[i] = transformed
	}
	return result, nil
}

// reverseFieldLookup finds the internal field name for a given external field name.
func reverseFieldLookup(fieldMap map[string]string, externalKey string) string {
	for internalKey, extKey := range fieldMap {
		if strings.EqualFold(extKey, externalKey) {
			return internalKey
		}
	}
	return ""
}

// filterByEq filters a User slice by matching an internal field value (case-insensitive).
// Used by get_user lookup_field fallback.
func filterByEq(users []User, internalField, value string) []User {
	var result []User
	for _, u := range users {
		src, err := structToMap(u)
		if err != nil {
			continue
		}
		v, ok := getNestedValue(src, internalField)
		if !ok {
			continue
		}
		if strings.EqualFold(fmt.Sprintf("%v", v), value) {
			result = append(result, u)
		}
	}
	return result
}
