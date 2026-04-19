# go_testapi

A feature-rich REST and SOAP test API server written in Go using the [Gin](https://github.com/gin-gonic/gin) web framework. Designed for integration testing, client development, and API exploration.

## Features

- REST API for **Users**, **Persons**, and **Contracts**
- **SOAP** endpoint with WSDL
- **OAuth2** client credentials flow + **Basic Auth** + optional **OpenID Connect SSO**
- **JSON and XML** content negotiation (request and response)
- **Pagination** with configurable page sizes
- **Rate limiting** per resource group (token-bucket algorithm)
- **CSV export** for all resources
- **File or in-memory persistence**
- **Seed data** on first startup
- HTML **API documentation** at `/docs`

---

## Getting Started

### Build

```bash
go build -o go_testapi .
```

### Run

```bash
./go_testapi                  # uses config.json in current directory
./go_testapi myconfig.json    # custom config path
```

### Health Check

```
GET /health  →  200 OK
```

---

## Configuration

Copy `config.template.json` to `config.json` and adjust as needed.

| Key | Default | Description |
|-----|---------|-------------|
| `port` | `8080` | Listening port |
| `client_id` | `test-client` | OAuth / Basic Auth client ID |
| `secret` | `test-secret` | OAuth / Basic Auth secret |
| `token_expiry_s` | `3600` | Bearer token lifetime in seconds |
| `seed_data` | `true` | Populate demo data on startup |
| `storage.type` | `memory` | `memory` or `file` |
| `storage.path` | `./data` | Directory for file persistence |
| `rate_limit.users.requests_per_10seconds` | `0` | Users rate limit (0 = disabled) |
| `rate_limit.users.burst` | `10` | Users burst size |
| `rate_limit.persons.requests_per_10seconds` | `0` | Persons/contracts rate limit |
| `rate_limit.persons.burst` | `10` | Persons/contracts burst size |
| `pagination.users.default_page_size` | `20` | Default page size for users |
| `pagination.users.max_page_size` | `100` | Max page size for users |
| `pagination.persons.default_page_size` | `20` | Default page size for persons |
| `pagination.persons.max_page_size` | `100` | Max page size for persons |

### OpenID Connect SSO (optional)

| Key | Description |
|-----|-------------|
| `oidc.discovery_url` | IdP discovery URL (leave empty to disable) |
| `oidc.client_id` | OIDC client ID |
| `oidc.client_secret` | OIDC client secret |
| `oidc.redirect_url` | Callback URL, e.g. `http://localhost:8080/oidc/callback` |
| `oidc.scopes` | Scopes array, defaults to `["openid","profile","email"]` |
| `oidc.permissions_claim` | Token claim containing groups/roles |
| `oidc.skip_signature_verification` | Skip ID token signature check (for HS256 IdPs) |

---

## Authentication

All `/api/*` endpoints require authentication. Public endpoints are listed below.

### Basic Auth

```
Authorization: Basic <base64(client_id:secret)>
```

### OAuth2 Client Credentials

```
POST /oauth/token
Content-Type: application/x-www-form-urlencoded

grant_type=client_credentials&client_id=test-client&client_secret=test-secret
```

Response:
```json
{
  "access_token": "...",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

Use the token as:
```
Authorization: Bearer <access_token>
```

### OpenID Connect SSO

When configured, the following routes are available:

| Route | Description |
|-------|-------------|
| `GET /oidc/login` | Redirect to IdP login |
| `GET /oidc/callback` | IdP callback |
| `GET /oidc/logout` | Clear session and log out |
| `GET /oidc/me` | HTML profile page with claims and permissions |

---

## REST API

### Content Negotiation

Responses default to JSON. To receive XML:
- Set `Accept: application/xml` header, or
- Append `?format=xml` to any request

Request bodies are auto-detected from `Content-Type` (JSON or XML).

### Pagination

All list endpoints accept:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `page` | `1` | Page number (1-indexed) |
| `page_size` | `20` | Items per page |

Paginated responses include a `pagination` object:

```json
{
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 150,
    "total_pages": 8,
    "next_page": "/api/users?page=2&page_size=20"
  }
}
```

---

### Users `/api/users`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/users` | List users (paginated) |
| `GET` | `/api/users/export` | Download CSV |
| `GET` | `/api/users/{id}` | Get user |
| `POST` | `/api/users` | Create user (`username` required) |
| `PUT` | `/api/users/{id}` | Update user |
| `DELETE` | `/api/users/{id}` | Delete user |
| `POST` | `/api/users/{id}/enable` | Enable user |
| `POST` | `/api/users/{id}/disable` | Disable user |
| `GET` | `/api/users/{id}/permissions` | List permissions |
| `POST` | `/api/users/{id}/permissions` | Add permission |
| `DELETE` | `/api/users/{id}/permissions/{permission}` | Remove permission |

**User object:**
```json
{
  "id": "string",
  "username": "string",
  "email": "string",
  "first_name": "string",
  "last_name": "string",
  "enabled": true,
  "permissions": ["read", "write"],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

---

### Persons `/api/persons`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/persons` | List persons (paginated) |
| `GET` | `/api/persons/export` | Download CSV |
| `GET` | `/api/persons/{id}` | Get person |
| `POST` | `/api/persons` | Create person (`first_name`, `last_name` required) |
| `PUT` | `/api/persons/{id}` | Update person |
| `DELETE` | `/api/persons/{id}` | Delete person (cascades to contracts) |

**Person object:**
```json
{
  "id": "string",
  "first_name": "string",
  "last_name": "string",
  "birthday": "1990-01-15",
  "address": {
    "street": "string",
    "city": "string",
    "state": "string",
    "zip": "string",
    "country": "string"
  },
  "phones": ["555-1234"],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

---

### Contracts `/api/contracts` and `/api/persons/{id}/contracts`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/contracts` | List all contracts (paginated) |
| `GET` | `/api/contracts/export` | Download CSV |
| `GET` | `/api/persons/{id}/contracts` | List contracts for a person |
| `GET` | `/api/persons/{id}/contracts/{contractId}` | Get contract |
| `POST` | `/api/persons/{id}/contracts` | Create contract (`company`, `title`, `start_date` required) |
| `PUT` | `/api/persons/{id}/contracts/{contractId}` | Update contract |
| `DELETE` | `/api/persons/{id}/contracts/{contractId}` | Delete contract |

**Contract object:**
```json
{
  "id": "string",
  "person_id": "string",
  "manager": "string",
  "department": "string",
  "company": "string",
  "title": "string",
  "start_date": "2023-03-01",
  "end_date": "2024-12-31",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

---

## SOAP API

**Endpoint:** `POST /soap` (requires authentication)  
**WSDL:** `GET /soap` or `GET /soap?wsdl`

Operations are dispatched by the root element name of the SOAP body or the `SOAPAction` header.

| Operation | Parameters |
|-----------|------------|
| `ListUsers` | — |
| `GetUser` | `Id` |
| `CreateUser` | `Username`, `Email`, `FirstName`, `LastName`, `Enabled` |
| `UpdateUser` | `Id`, editable fields |
| `DeleteUser` | `Id` |
| `EnableUser` / `DisableUser` | `Id` |
| `GetPermissions` | `Id` |
| `AddPermission` / `RemovePermission` | `Id`, `Permission` |
| `ListPersons` | — |
| `GetPerson` | `Id` |
| `CreatePerson` | `FirstName`, `LastName`, `Birthday`, `Street`, `City`, `State`, `Zip`, `Country`, `Phone` |
| `UpdatePerson` | `Id`, editable fields |
| `DeletePerson` | `Id` |
| `ListContracts` | `Id` (personId) |
| `GetContract` | `Id` (personId), `ContractId` |
| `CreateContract` | `Id`, `Company`, `Title`, `StartDate`, `Manager`, `Department`, `EndDate` |
| `UpdateContract` | `Id`, `ContractId`, editable fields |
| `DeleteContract` | `Id`, `ContractId` |

Example request:
```xml
POST /soap
Content-Type: text/xml
Authorization: Basic dGVzdC1jbGllbnQ6dGVzdC1zZWNyZXQ=

<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <ListUsers/>
  </soap:Body>
</soap:Envelope>
```

---

## Persistence

| Mode | Description |
|------|-------------|
| `memory` | All data is in-memory; lost on restart. Good for testing. |
| `file` | Data written to `storage.path` as JSON files (`users.json`, `persons.json`). Atomic writes via temp file + rename. |

---

## Seed Data

When `seed_data: true`, the server populates demo data on startup (only if the store is empty):

**Users:** alice (read + write), bob (read), charlie (disabled)

**Persons/Contracts:** John Doe — Senior Developer at Acme Corp; Mary Johnson — Marketing Lead then Sales Manager at Globex

---

## Public Routes (no auth required)

| Route | Description |
|-------|-------------|
| `GET /health` | Liveness probe |
| `GET /docs` | HTML API documentation |
| `GET /soap` | WSDL document |
| `POST /oauth/token` | OAuth2 token endpoint |
