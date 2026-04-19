package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

// ============================================================
// Session
// ============================================================

type oidcSession struct {
	Claims      map[string]interface{}
	Permissions []string  // extracted from PermissionsClaim
	ExpiresAt   time.Time // zero = never checked
}

// ============================================================
// Service
// ============================================================

// OIDCService handles browser-based OpenID Connect SSO.
type OIDCService struct {
	cfg      OIDCConfig
	provider *gooidc.Provider
	oauth2   oauth2.Config
	verifier *gooidc.IDTokenVerifier

	mu       sync.RWMutex
	sessions map[string]*oidcSession
	states   map[string]time.Time // anti-CSRF nonce → expiry
}

// NewOIDCService fetches the provider metadata via OIDC discovery.
// Returns (nil, nil) when DiscoveryURL is empty (feature disabled).
func NewOIDCService(cfg OIDCConfig, _ *AuthService) (*OIDCService, error) {
	if cfg.DiscoveryURL == "" {
		return nil, nil
	}
	ctx := context.Background()
	provider, err := gooidc.NewProvider(ctx, cfg.DiscoveryURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery at %q failed: %w", cfg.DiscoveryURL, err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{gooidc.ScopeOpenID, "profile", "email"}
	}

	return &OIDCService{
		cfg:      cfg,
		provider: provider,
		oauth2: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
		verifier: provider.Verifier(&gooidc.Config{
			ClientID:                   cfg.ClientID,
			InsecureSkipSignatureCheck: cfg.SkipSignatureVerification,
		}),

		sessions: make(map[string]*oidcSession),
		states:   make(map[string]time.Time),
	}, nil
}

// ============================================================
// Handlers
// ============================================================

// GET /oidc/login — redirect the browser to the IdP.
func (o *OIDCService) LoginHandler(c *gin.Context) {
	state := newID()
	o.mu.Lock()
	o.states[state] = time.Now().Add(5 * time.Minute)
	o.mu.Unlock()
	c.SetCookie("oidc_state", state, 300, "/", "", false, true)
	c.Redirect(http.StatusFound, o.oauth2.AuthCodeURL(state))
}

// GET /oidc/callback — IdP redirects here with ?code=…&state=…
func (o *OIDCService) CallbackHandler(c *gin.Context) {
	// Validate anti-CSRF state.
	stateCookie, _ := c.Cookie("oidc_state")
	if stateCookie == "" || c.Query("state") != stateCookie {
		respondMsg(c, http.StatusBadRequest, "OIDC state mismatch — possible CSRF")
		return
	}
	o.mu.Lock()
	_, stateKnown := o.states[stateCookie]
	delete(o.states, stateCookie)
	o.mu.Unlock()
	if !stateKnown {
		respondMsg(c, http.StatusBadRequest, "OIDC state expired or unknown")
		return
	}

	ctx := context.Background()
	tok, err := o.oauth2.Exchange(ctx, c.Query("code"))
	if err != nil {
		respondMsg(c, http.StatusInternalServerError, "token exchange failed: "+err.Error())
		return
	}

	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok {
		respondMsg(c, http.StatusInternalServerError, "no id_token in token response")
		return
	}

	var claims map[string]interface{}
	if o.cfg.SkipSignatureVerification {
		// IdP uses HS256 (symmetric) — go-oidc cannot verify that algorithm.
		// Parse the payload manually and validate iss/aud/exp ourselves.
		var parseErr error
		claims, parseErr = parseJWTPayload(rawIDToken, o.cfg.DiscoveryURL, o.cfg.ClientID)
		if parseErr != nil {
			respondMsg(c, http.StatusInternalServerError, "id_token parse failed: "+parseErr.Error())
			return
		}
	} else {
		idToken, verifyErr := o.verifier.Verify(ctx, rawIDToken)
		if verifyErr != nil {
			respondMsg(c, http.StatusInternalServerError, "id_token verification failed: "+verifyErr.Error())
			return
		}
		if claimErr := idToken.Claims(&claims); claimErr != nil {
			respondMsg(c, http.StatusInternalServerError, "claims parse failed: "+claimErr.Error())
			return
		}
	}

	sess := &oidcSession{
		Claims:      claims,
		Permissions: claimAsStrings(claims, o.cfg.PermissionsClaim),
		ExpiresAt:   tok.Expiry,
	}
	sessID := newID()
	o.mu.Lock()
	o.sessions[sessID] = sess
	o.mu.Unlock()

	c.SetCookie("oidc_session", sessID, 3600, "/", "", false, true)
	c.Redirect(http.StatusFound, "/oidc/me")
}

// GET /oidc/logout — clear session and redirect to root.
func (o *OIDCService) LogoutHandler(c *gin.Context) {
	if sid, err := c.Cookie("oidc_session"); err == nil {
		o.mu.Lock()
		delete(o.sessions, sid)
		o.mu.Unlock()
	}
	c.SetCookie("oidc_session", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}

// GET /oidc/me — HTML profile + permissions page.
func (o *OIDCService) MeHandler(c *gin.Context) {
	sess := o.sessionFromCookie(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/oidc/login")
		return
	}
	data := struct {
		Claims           map[string]interface{}
		Permissions      []string
		PermissionsClaim string
	}{
		Claims:           sess.Claims,
		Permissions:      sess.Permissions,
		PermissionsClaim: o.cfg.PermissionsClaim,
	}
	tmpl := template.Must(template.New("me").Funcs(template.FuncMap{
		"stringify": func(v interface{}) string { return fmt.Sprintf("%v", v) },
	}).Parse(mePage))
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(c.Writer, data)
}

// ============================================================
// Helpers
// ============================================================

func (o *OIDCService) sessionFromCookie(c *gin.Context) *oidcSession {
	sid, err := c.Cookie("oidc_session")
	if err != nil || sid == "" {
		return nil
	}
	o.mu.RLock()
	sess := o.sessions[sid]
	o.mu.RUnlock()
	if sess == nil {
		return nil
	}
	if !sess.ExpiresAt.IsZero() && time.Now().After(sess.ExpiresAt) {
		o.mu.Lock()
		delete(o.sessions, sid)
		o.mu.Unlock()
		return nil
	}
	return sess
}

// claimAsStrings extracts a claim as a []string. Handles:
//   - []interface{} of strings — standard OIDC groups claim
//   - single string — comma-separated or space-separated list
func claimAsStrings(claims map[string]interface{}, key string) []string {
	if key == "" {
		return nil
	}
	val, ok := claims[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		sep := ","
		if !strings.Contains(v, ",") {
			sep = " "
		}
		var out []string
		for _, p := range strings.Split(v, sep) {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// parseJWTPayload decodes a JWT payload without verifying the signature.
// Used for HS256-signed tokens (e.g. Hello ID) where go-oidc cannot verify
// the symmetric key. Validates issuer, audience, and expiry manually.
func parseJWTPayload(rawToken, issuer, clientID string) (map[string]interface{}, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed JWT: expected 3 parts, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("JWT payload decode: %w", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("JWT claims unmarshal: %w", err)
	}

	// Validate issuer.
	if iss, _ := claims["iss"].(string); iss != issuer {
		return nil, fmt.Errorf("issuer mismatch: got %q, want %q", iss, issuer)
	}

	// Validate audience (string or []interface{}).
	audOK := false
	switch v := claims["aud"].(type) {
	case string:
		audOK = v == clientID
	case []interface{}:
		for _, a := range v {
			if s, ok := a.(string); ok && s == clientID {
				audOK = true
				break
			}
		}
	}
	if !audOK {
		return nil, fmt.Errorf("audience does not contain client ID %q", clientID)
	}

	// Validate expiry.
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return nil, fmt.Errorf("token has expired")
		}
	}

	return claims, nil
}

// ============================================================
// HTML template for /oidc/me
// ============================================================

const mePage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Signed-in user — go_testapi</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:'Segoe UI',system-ui,sans-serif;background:#0f1117;color:#c9d1d9;min-height:100vh;padding:2rem}
  h1{font-size:1.4rem;font-weight:600;color:#e6edf3;margin-bottom:1.5rem}
  h2{font-size:1rem;font-weight:600;color:#8b949e;text-transform:uppercase;letter-spacing:.08em;margin:1.5rem 0 .75rem}
  .card{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:1.25rem 1.5rem;margin-bottom:1rem}
  .profile{display:flex;align-items:center;gap:1.25rem}
  .avatar{width:64px;height:64px;border-radius:50%;border:2px solid #30363d;object-fit:cover}
  .avatar-placeholder{width:64px;height:64px;border-radius:50%;background:#21262d;border:2px solid #30363d;display:flex;align-items:center;justify-content:center;font-size:1.8rem;color:#8b949e}
  .name{font-size:1.2rem;font-weight:600;color:#e6edf3}
  .email{font-size:.875rem;color:#8b949e;margin-top:.2rem}
  table{width:100%;border-collapse:collapse;font-size:.85rem}
  th,td{padding:.5rem .75rem;text-align:left;border-bottom:1px solid #21262d}
  th{color:#8b949e;font-weight:500;width:35%}
  td{color:#c9d1d9;word-break:break-all;font-family:monospace}
  .tag{display:inline-block;background:#1f6feb22;border:1px solid #1f6feb55;color:#58a6ff;border-radius:4px;padding:.15rem .5rem;font-size:.8rem;margin:.15rem .15rem .15rem 0}
  .empty{color:#484f58;font-style:italic;font-size:.85rem}
  .btn{display:inline-block;padding:.5rem 1.25rem;background:#238636;color:#fff;border:none;border-radius:6px;cursor:pointer;font-size:.875rem;text-decoration:none;margin-right:.5rem}
  .btn:hover{background:#2ea043}
  .btn-danger{background:#da3633}
  .btn-danger:hover{background:#f85149}
  .btn-secondary{background:#21262d;border:1px solid #30363d;color:#c9d1d9}
  .btn-secondary:hover{background:#30363d}
  .token-box{background:#0d1117;border:1px solid #30363d;border-radius:6px;padding:.75rem 1rem;font-family:monospace;font-size:.8rem;color:#58a6ff;word-break:break-all;margin-top:.75rem;display:none}
  .token-perms{margin-top:.5rem;font-size:.8rem;color:#8b949e}
  #token-error{color:#f85149;font-size:.85rem;margin-top:.5rem;display:none}
  .actions{margin-top:1.5rem;display:flex;gap:.5rem;flex-wrap:wrap}
</style>
</head>
<body>
<h1>Signed-in user</h1>

<!-- Profile card -->
<div class="card">
  <div class="profile">
    {{if index .Claims "picture"}}
      <img class="avatar" src="{{stringify (index .Claims "picture")}}" alt="avatar">
    {{else}}
      <div class="avatar-placeholder">&#128100;</div>
    {{end}}
    <div>
      <div class="name">{{if index .Claims "name"}}{{stringify (index .Claims "name")}}{{else}}Unknown{{end}}</div>
      <div class="email">{{if index .Claims "email"}}{{stringify (index .Claims "email")}}{{else}}no email claim{{end}}</div>
    </div>
  </div>
</div>

<!-- Permissions / group memberships -->
<h2>{{if .PermissionsClaim}}Permissions (claim: <code>{{.PermissionsClaim}}</code>){{else}}Permissions{{end}}</h2>
<div class="card">
  {{if .Permissions}}
    {{range .Permissions}}<span class="tag">{{.}}</span>{{end}}
  {{else if .PermissionsClaim}}
    <span class="empty">No values found in claim &ldquo;{{.PermissionsClaim}}&rdquo;. Check your IdP attribute mapping.</span>
  {{else}}
    <span class="empty">permissions_claim not configured.</span>
  {{end}}
</div>

<!-- All claims -->
<h2>ID token claims</h2>
<div class="card">
  <table>
    <thead><tr><th>Claim</th><th>Value</th></tr></thead>
    <tbody>
      {{range $k,$v := .Claims}}
      <tr><td>{{$k}}</td><td>{{stringify $v}}</td></tr>
      {{end}}
    </tbody>
  </table>
</div>

<div class="actions">
  <a class="btn btn-danger" href="/oidc/logout">Sign out</a>
</div>
</body>
</html>`
