package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ---- token store ----

type tokenEntry struct {
	expiresAt time.Time
}

// AuthService handles Basic Auth validation and OAuth2 client-credentials flow.
type AuthService struct {
	cfg    Config
	mu     sync.RWMutex
	tokens map[string]tokenEntry
}

func NewAuthService(cfg Config) *AuthService {
	return &AuthService{
		cfg:    cfg,
		tokens: make(map[string]tokenEntry),
	}
}

// ---- OAuth token endpoint  POST /oauth/token ----

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (a *AuthService) TokenHandler(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	grantType := c.PostForm("grant_type")
	if grantType != "client_credentials" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "unsupported_grant_type",
			"error_description": "only client_credentials is supported",
		})
		return
	}

	// Accept client_id/client_secret from body OR Basic Auth header.
	clientID := c.PostForm("client_id")
	secret := c.PostForm("client_secret")
	if clientID == "" {
		clientID, secret, _ = c.Request.BasicAuth()
	}

	if clientID != a.cfg.ClientID || secret != a.cfg.Secret {
		c.Header("WWW-Authenticate", `Basic realm="testapi"`)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_client"})
		return
	}

	token := newID() + newID() // 32 hex chars
	a.mu.Lock()
	a.tokens[token] = tokenEntry{expiresAt: time.Now().Add(time.Duration(a.cfg.TokenExpiry) * time.Second)}
	a.mu.Unlock()

	c.JSON(http.StatusOK, tokenResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   a.cfg.TokenExpiry,
	})
}

// ---- Gin middleware ----

// Middleware validates either Basic Auth or a Bearer token.
func (a *AuthService) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a.checkAuth(c.Request) {
			c.Next()
			return
		}
		c.Header("WWW-Authenticate", `Basic realm="testapi", Bearer realm="testapi"`)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

func (a *AuthService) checkAuth(r *http.Request) bool {
	// Basic Auth
	if clientID, secret, ok := r.BasicAuth(); ok {
		return clientID == a.cfg.ClientID && secret == a.cfg.Secret
	}
	// Bearer token
	if token := extractBearer(r); token != "" {
		a.mu.RLock()
		entry, ok := a.tokens[token]
		a.mu.RUnlock()
		return ok && time.Now().Before(entry.expiresAt)
	}
	return false
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return after
	}
	return ""
}

// IssueToken mints a new Bearer token with the configured expiry and stores it.
// Used by the OIDC service to give logged-in SSO users an API token.
func (a *AuthService) IssueToken() (token string, expiry time.Time) {
	token = newID() + newID()
	expiry = time.Now().Add(time.Duration(a.cfg.TokenExpiry) * time.Second)
	a.mu.Lock()
	a.tokens[token] = tokenEntry{expiresAt: expiry}
	a.mu.Unlock()
	return
}

// ---- low-level helpers (used by soap.go which writes raw http) ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
