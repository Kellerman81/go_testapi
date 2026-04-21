# Profile File Guide

A profile file lets you expose go_testapi's internal store under a completely different API shape — different paths, field names, HTTP status codes, and response envelopes — so you can run real connector scripts against it by only changing the base domain.

Set the active profile in `config.json`:
```json
{ "profile_path": "./profiles/helloid.json" }
```

---

## Top-level structure

```json
{
  "name": "my-profile",
  "routes": [ ... ]
}
```

Each entry in `routes` maps one external HTTP endpoint to one internal action.

---

## Route fields

| Field | Type | Description |
|---|---|---|
| `method` | string | HTTP method: `GET`, `POST`, `PUT`, `PATCH`, `DELETE` |
| `path` | string | URL path. Params: `:id`, `:groupId`, `:userId`, `:contractId` |
| `action` | string | What to do (see Actions below) |
| `status` | int | Override the default response status code |
| `lookup_field` | string | Internal field to search when `:id` lookup fails (e.g. `"username"`) |
| `chain_field` | string | After `get_user`, read this field and return that user instead (e.g. `"manager"`) |
| `target_field` | string | Internal field to write for `set_user_field` / `clear_user_field` |
| `user_body_field` | string | JSON key in POST body that holds the user ID (for `link_group`, `set_user_field`) |
| `user_body_extract` | string | How to extract the value: `""` = use as-is, `"last_path_segment"` = last URL segment |
| `input` | object | Request body mapping — see Input below |
| `output` | object | Response shaping — see Output below |
| `body` | object | Static JSON body returned by the `static` action |
| `limiter` | string | Rate-limiter bucket: `"user"` (default) or `"person"` |

---

## Actions

### User actions

| Action | Default status | Description |
|---|---|---|
| `list_users` | 200 | Return all users |
| `get_user` | 200 | Get one user by `:id`. Use `lookup_field` to also match by another field (e.g. username) |
| `create_user` | 201 | Create a user from request body |
| `update_user` | 200 | Update a user. Requires `input`. Returns updated user unless `status: 204` |
| `delete_user` | 200 | Delete a user |
| `enable_user` | 200 | Set `enabled = true`. Add `output` to return the updated user |
| `disable_user` | 200 | Set `enabled = false`. Add `output` to return the updated user |
| `set_user_field` | 200 | Read a value from the body and write it to `target_field` on the user |
| `clear_user_field` | 200 | Clear `target_field` to empty string on the user |

### Group actions (map to user permissions)

| Action | Default status | Description |
|---|---|---|
| `list_groups` | 200 | Return all unique permissions as group objects |
| `link_group` | 200 | Add user to group. Body field configured via `user_body_field` + `user_body_extract` |
| `unlink_group` | 200 | Remove user from group. Reads `:groupId` and `:userId` from path |

### Person / Contract actions

| Action | Default status | Description |
|---|---|---|
| `list_persons` | 200 | List all persons |
| `get_person` | 200 | Get one person by `:id` |
| `create_person` | 201 | Create a person |
| `update_person` | 200 | Update a person |
| `delete_person` | 200 | Delete a person |
| `list_contracts` | 200 | List contracts for person `:id` |
| `get_contract` | 200 | Get contract `:contractId` for person `:id` |
| `create_contract` | 201 | Create a contract for person `:id` |
| `update_contract` | 200 | Update a contract |
| `delete_contract` | 200 | Delete a contract |

### Utility

| Action | Default status | Description |
|---|---|---|
| `static` | 200 | Return a fixed status (and optional `body`). Use for no-op endpoints |

---

## Input mapping

`input` tells the engine how to read an external request body into the internal store.

| Field | Description |
|---|---|
| `field_map` | `{ "internal_field": "ExternalBodyKey" }` — which external key maps to which internal field |
| `item_key` | If the body wraps fields under a key (e.g. `"attributes"`), unwrap it first |
| `attribute_style` | `"labeled"` — body values are `{"label":"...","value":...,"type":"..."}` objects; extract `value` automatically |

**Plain body** (HelloID `PUT /users/:id`):
```json
{ "userName": "jdoe", "emailAddress": "j@example.com", "isEnabled": true }
```
```json
"input": {
  "field_map": { "username": "userName", "email": "emailAddress", "enabled": "isEnabled" }
}
```

**Nested body** (Personio `POST /employees`, fields inside `"attributes"`):
```json
{ "attributes": { "first_name": {"label":"First Name","value":"Jane","type":"standard"} } }
```
```json
"input": {
  "item_key": "attributes",
  "attribute_style": "labeled",
  "field_map": { "first_name": "first_name", "last_name": "last_name" }
}
```

### Internal field names

**User:** `id`, `username` *(required on create)*, `email`, `first_name`, `last_name`, `enabled` *(bool — false is a valid update)*, `manager` *(another user's ID)*

**Person:** `id`, `first_name`, `last_name`, `birthday`, `address.street`, `address.city`, `address.state`, `address.zip`, `address.country`

**Contract:** `id`, `person_id`, `company`, `title`, `department`, `manager`, `start_date`, `end_date`

---

## Output mapping

`output` tells the engine how to shape the internal data into the external response.

**Always:**

| Field | Description |
|---|---|
| `field_map` | `{ "internal_field": "ExternalKey" }` — renames fields in the response |
| `attribute_style` | `"labeled"` — wraps each value as `{"label":"...","value":...,"type":"..."}` |

**Per item** (get, create, update — and each item inside a list):

| Field | Description |
|---|---|
| `item_key` | Nest the mapped fields under this key (e.g. `"attributes"`) |
| `item_extra` | Fixed fields merged into the item alongside `item_key` (e.g. `{"type":"Employee"}`) |

**List wrapper** (list actions only):

| Field | Description |
|---|---|
| `list_key` | Key that wraps the items array (e.g. `"value"`, `"data"`). Default: `"items"` |
| `list_extra` | Fixed fields merged into the wrapper object (e.g. `{"@odata.context":"..."}`) |
| `raw` | `true` — skip the wrapper entirely and return a plain JSON array |

**Group list** (`list_groups` only):

| Field | Description |
|---|---|
| `id_field` | Key for the group ID in each group object. Default: `"id"` |
| `name_field` | Key for the group name in each group object. Default: `"name"` |

**Date formatting:**

| Field | Description |
|---|---|
| `field_formats` | `{ "ExternalKey": "pattern" }` — reformats a date/datetime field using the given pattern |

Reserved type conversions (use instead of a date pattern):

| Value | Converts |
|---|---|
| `"int"` | string/float → integer number |
| `"float"` | string/int → floating-point number |
| `"string"` | any value → string |

Pattern tokens (for date formatting):

| Token | Meaning | Example output |
|---|---|---|
| `yyyy` | 4-digit year | `2014` |
| `MM` | 2-digit month | `11` |
| `dd` | 2-digit day | `09` |
| `HH` | 2-digit hour (24h) | `00` |
| `mm` | 2-digit minute | `00` |
| `ss` | 2-digit second | `00` |
| `Z` | Timezone offset | `+00:00` |

Wrap literal characters in single quotes to prevent them being treated as tokens: `'T'`, `'Z'`.

The input value is parsed automatically from `YYYY-MM-DD` or `YYYY-MM-DDT...` — if parsing fails the value is passed through unchanged.

```json
"field_formats": {
  "start_date": "yyyy-MM-dd'T'HH:mm:ssZ",
  "end_date":   "dd.MM.yyyy"
}
```
`"2014-11-09"` → `"2014-11-09T00:00:00+00:00"` and `"09.11.2014"` respectively.

**Flat response** (HelloID `GET /users/:id`):
```json
{ "userGUID": "abc", "userName": "jdoe", "isEnabled": true, "userAttributes": {}, "managedByUserGUID": null }
```
```json
"output": {
  "item_extra": { "userAttributes": {}, "managedByUserGUID": null },
  "field_map": { "id": "userGUID", "username": "userName", "enabled": "isEnabled" }
}
```

**OData list wrapper** (Entra `GET /v1.0/users`):
```json
{ "@odata.context": "https://...", "value": [ { "id": "abc", "userPrincipalName": "jdoe" } ] }
```
```json
"output": {
  "list_key":   "value",
  "list_extra": { "@odata.context": "https://graph.microsoft.com/v1.0/$metadata#users" },
  "field_map":  { "id": "id", "username": "userPrincipalName", "enabled": "accountEnabled" }
}
```

**Raw array** (HelloID `GET /users`):
```json
[ { "userGUID": "abc", "userName": "jdoe" } ]
```
```json
"output": {
  "raw": true,
  "field_map": { "id": "userGUID", "username": "userName" }
}
```

**Nested labeled attributes** (Personio `GET /employees/:id`):
```json
{ "type": "Employee", "attributes": { "first_name": {"label":"First Name","value":"Jane","type":"standard"} } }
```
```json
"output": {
  "item_key":        "attributes",
  "item_extra":      { "type": "Employee" },
  "attribute_style": "labeled",
  "field_map":       { "first_name": "first_name", "last_name": "last_name" }
}
```

---

## Examples

### Simple flat rename (HelloID style)

```json
{
  "method": "GET",
  "path":   "/users/:id",
  "action": "get_user",
  "lookup_field": "username",
  "output": {
    "item_extra": { "userAttributes": {}, "managedByUserGUID": null },
    "field_map": {
      "id":         "userGUID",
      "username":   "userName",
      "enabled":    "isEnabled",
      "email":      "emailAddress",
      "first_name": "firstName",
      "last_name":  "lastName"
    }
  }
}
```

### OData list envelope (Entra / Microsoft Graph style)

```json
{
  "method": "GET",
  "path":   "/v1.0/users",
  "action": "list_users",
  "output": {
    "list_key":   "value",
    "list_extra": { "@odata.context": "https://graph.microsoft.com/v1.0/$metadata#users" },
    "field_map": {
      "id":         "id",
      "username":   "userPrincipalName",
      "enabled":    "accountEnabled"
    }
  }
}
```

### Labeled attributes (Personio style)

```json
{
  "method": "GET",
  "path":   "/api/v1/company/employees/:id",
  "action": "get_person",
  "limiter": "person",
  "output": {
    "item_key":        "attributes",
    "item_extra":      { "type": "Employee" },
    "attribute_style": "labeled",
    "field_map": {
      "id":         "id",
      "first_name": "first_name",
      "last_name":  "last_name"
    }
  }
}
```
Response:
```json
{ "type": "Employee", "attributes": { "id": {"label":"Id","value":"abc","type":"standard"}, ... } }
```

### Group membership (Entra style, @odata.id body)

```json
{
  "method": "POST",
  "path":             "/v1.0/groups/:groupId/members/$ref",
  "action":           "link_group",
  "user_body_field":  "@odata.id",
  "user_body_extract": "last_path_segment",
  "status": 204
}
```
Body: `{ "@odata.id": "https://graph.microsoft.com/v1.0/directoryObjects/user-id-here" }`

### Chained lookup — return a related user (manager)

```json
{
  "method": "GET",
  "path":        "/v1.0/users/:id/manager",
  "action":      "get_user",
  "chain_field": "manager",
  "output": { "field_map": { "id": "id", "username": "userPrincipalName" } }
}
```
Looks up user `:id`, reads its `manager` field (which is another user's ID), then returns that user.

### Set a field from a URL body value

```json
{
  "method":           "PUT",
  "path":             "/v1.0/users/:id/manager/$ref",
  "action":           "set_user_field",
  "target_field":     "manager",
  "user_body_field":  "@odata.id",
  "user_body_extract": "last_path_segment",
  "status": 204
}
```

### No-op endpoint (static)

```json
{ "method": "DELETE", "path": "/v1.0/users/:id/manager/$ref", "action": "static", "status": 204 }
```

### Static endpoint with a response body

```json
{
  "method": "GET",
  "path":   "/health",
  "action": "static",
  "status": 200,
  "body":   { "status": "ok" }
}
```
