package main

import (
	"encoding/json"
	"log"
	"os"
)

// ---- storage ----

// StorageConfig controls where data is persisted between restarts.
type StorageConfig struct {
	// Type: "memory" (default) or "file"
	Type string `json:"type"`
	// Path is the data directory when Type is "file". Defaults to "./data".
	Path string `json:"path"`
}

// ---- rate limiting ----

// RateLimitGroupConfig configures the token-bucket limiter for one resource group.
type RateLimitGroupConfig struct {
	// RequestsPer10Seconds is the number of requests allowed per 10-second window.
	// 0 or negative disables rate limiting for the group.
	RequestsPer10Seconds float64 `json:"requests_per_10seconds"`
	// Burst is the maximum number of requests that can be made in a single
	// instant (token-bucket burst size). Must be >= 1 when limiting is enabled.
	Burst int `json:"burst"`
}

// RateLimitConfig holds per-resource-group rate limit settings.
type RateLimitConfig struct {
	Users   RateLimitGroupConfig `json:"users"`
	Persons RateLimitGroupConfig `json:"persons"`
}

// ---- pagination ----

// PaginationGroupConfig controls paging defaults for one resource group.
type PaginationGroupConfig struct {
	// DefaultPageSize is used when the caller omits ?page_size. Defaults to 20.
	DefaultPageSize int `json:"default_page_size"`
	// MaxPageSize caps the value a caller can request. Defaults to 100.
	MaxPageSize int `json:"max_page_size"`
}

// PaginationConfig holds per-resource-group pagination settings.
type PaginationConfig struct {
	Users   PaginationGroupConfig `json:"users"`
	Persons PaginationGroupConfig `json:"persons"`
}

// ---- OpenID Connect SSO ----

// OIDCConfig enables browser-based SSO via any OpenID Connect provider.
// Leave DiscoveryURL empty to disable the feature.
type OIDCConfig struct {
	// DiscoveryURL is the base URL of the IdP. The server appends
	// /.well-known/openid-configuration to fetch provider metadata.
	// Example: "https://accounts.google.com"
	DiscoveryURL string `json:"discovery_url"`

	// ClientID and ClientSecret are the app credentials registered at the IdP.
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`

	// RedirectURL must match the callback URL registered at the IdP.
	// Example: "http://localhost:8080/oidc/callback"
	RedirectURL string `json:"redirect_url"`

	// Scopes to request. Defaults to ["openid","profile","email"].
	// Add provider-specific scopes to receive group claims (e.g. "groups").
	Scopes []string `json:"scopes"`

	// PermissionsClaim is the name of the ID-token claim that carries the
	// user's group memberships / roles (e.g. "memberOf", "groups", "roles").
	// The claim value may be a JSON array of strings or a comma-separated string.
	// Leave empty to skip permission extraction.
	PermissionsClaim string `json:"permissions_claim"`

	// SkipSignatureVerification disables ID-token signature verification.
	// Required when the IdP signs tokens with HS256 (shared secret) instead of
	// RS256/ES256 (public key), because the JWKS endpoint does not expose
	// symmetric keys. Hello ID uses HS256 — set this to true for Hello ID.
	// Claims (issuer, audience, expiry) are still validated.
	SkipSignatureVerification bool `json:"skip_signature_verification"`
}

// ---- top-level config ----

// Config holds all runtime configuration for the server.
type Config struct {
	Port        int              `json:"port"`
	ClientID    string           `json:"client_id"`
	Secret      string           `json:"secret"`
	TokenExpiry int              `json:"token_expiry_s"`
	SeedData    bool             `json:"seed_data"`
	Storage     StorageConfig    `json:"storage"`
	RateLimit   RateLimitConfig  `json:"rate_limit"`
	Pagination  PaginationConfig `json:"pagination"`
	OIDC        OIDCConfig       `json:"oidc"`
	// ProfilePath is the path to a profile JSON file that adds alternative
	// endpoints shaped like another API (e.g. Personio, Entra). Leave empty to disable.
	ProfilePath string `json:"profile_path"`
}

func defaultConfig() Config {
	return Config{
		Port:        8080,
		ClientID:    "test-client",
		Secret:      "test-secret",
		TokenExpiry: 3600,
		SeedData:    true,
		Storage: StorageConfig{
			Type: "memory",
			Path: "./data",
		},
		RateLimit: RateLimitConfig{
			Users:   RateLimitGroupConfig{RequestsPer10Seconds: 0, Burst: 10},
			Persons: RateLimitGroupConfig{RequestsPer10Seconds: 0, Burst: 10},
		},
		Pagination: PaginationConfig{
			Users:   PaginationGroupConfig{DefaultPageSize: 20, MaxPageSize: 100},
			Persons: PaginationGroupConfig{DefaultPageSize: 20, MaxPageSize: 100},
		},
	}
}

// LoadConfig reads config from path. Missing file returns defaults.
func LoadConfig(path string) Config {
	cfg := defaultConfig()
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("config: cannot open %s: %v — using defaults", path, err)
		}
		return cfg
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		log.Printf("config: parse error in %s: %v — using defaults", path, err)
	}
	// Apply sub-field defaults
	if cfg.Storage.Type == "" {
		cfg.Storage.Type = "memory"
	}
	if cfg.Storage.Path == "" {
		cfg.Storage.Path = "./data"
	}
	if cfg.Pagination.Users.DefaultPageSize <= 0 {
		cfg.Pagination.Users.DefaultPageSize = 20
	}
	if cfg.Pagination.Users.MaxPageSize <= 0 {
		cfg.Pagination.Users.MaxPageSize = 100
	}
	if cfg.Pagination.Persons.DefaultPageSize <= 0 {
		cfg.Pagination.Persons.DefaultPageSize = 20
	}
	if cfg.Pagination.Persons.MaxPageSize <= 0 {
		cfg.Pagination.Persons.MaxPageSize = 100
	}
	if cfg.RateLimit.Users.Burst <= 0 && cfg.RateLimit.Users.RequestsPer10Seconds > 0 {
		cfg.RateLimit.Users.Burst = 1
	}
	if cfg.RateLimit.Persons.Burst <= 0 && cfg.RateLimit.Persons.RequestsPer10Seconds > 0 {
		cfg.RateLimit.Persons.Burst = 1
	}
	return cfg
}
