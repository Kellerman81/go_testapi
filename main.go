package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	cfgPath := "config.json"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	cfg := LoadConfig(cfgPath)

	dataDir := resolveDataDir(cfg)
	store := NewStore(dataDir, cfg.CustomFields.Users)
	personStore := NewPersonStore(dataDir, cfg.CustomFields.Persons, cfg.CustomFields.Contracts)
	if cfg.SeedData {
		store.Seed()
		personStore.SeedPersons()
	}

	auth := NewAuthService(cfg)
	users := NewUserHandler(store, cfg.Pagination.Users)
	persons := NewPersonHandler(personStore, cfg.Pagination.Persons)
	soap := NewSOAPHandler(store, personStore)

	userLimiter   := NewResourceLimiter(cfg.RateLimit.Users)
	personLimiter := NewResourceLimiter(cfg.RateLimit.Persons)

	oidcSvc, err := NewOIDCService(cfg.OIDC, auth)
	if err != nil {
		log.Fatalf("OIDC init: %v", err)
	}

	profile, profileErr := LoadProfile(cfg.ProfilePath)
	if profileErr != nil {
		log.Fatalf("profile: %v", profileErr)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	if cfg.LogFile != "" {
		logger, logFile, err := SetupLogger(cfg.LogFile)
		if err != nil {
			log.Fatalf("log file: %v", err)
		}
		defer logFile.Close()
		r.Use(RequestLogger(logger))
	}

	// ---- unauthenticated ----
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/docs", func(c *gin.Context) { c.File("docs.html") })
	r.POST("/oauth/token", auth.TokenHandler)
	r.GET("/soap", soap.WSDLHandler)

	// ---- OIDC SSO (optional) ----
	if oidcSvc != nil {
		r.GET("/oidc/login",    oidcSvc.LoginHandler)
		r.GET("/oidc/callback", oidcSvc.CallbackHandler)
		r.GET("/oidc/logout",   oidcSvc.LogoutHandler)
		r.GET("/oidc/me",       oidcSvc.MeHandler)
	}

	// ---- authenticated REST ----
	api := r.Group("/api", auth.Middleware())
	{
		// Users  (auth + user rate limiter)
		u := api.Group("/users", userLimiter.Middleware())
		u.GET("",                       users.ListUsers)
		u.GET("/export",                users.ExportUsers)
		u.POST("",                      users.CreateUser)
		u.GET("/:id",                   users.GetUser)
		u.PUT("/:id",                   users.UpdateUser)
		u.DELETE("/:id",                users.DeleteUser)
		u.POST("/:id/enable",           users.EnableUser)
		u.POST("/:id/disable",          users.DisableUser)
		u.GET("/:id/permissions",       users.GetPermissions)
		u.POST("/:id/permissions",      users.AddPermission)
		u.DELETE("/:id/permissions/:permission", users.RemovePermission)

		// Persons & contracts  (auth + person rate limiter)
		p := api.Group("", personLimiter.Middleware())
		p.GET("/persons",                                    persons.ListPersons)
		p.GET("/persons/export",                             persons.ExportPersons)
		p.GET("/contracts/export",                           persons.ExportContracts)
		p.POST("/persons",                                   persons.CreatePerson)
		p.GET("/persons/:id",                                persons.GetPerson)
		p.PUT("/persons/:id",                                persons.UpdatePerson)
		p.DELETE("/persons/:id",                             persons.DeletePerson)
		p.GET("/persons/:id/contracts",                      persons.ListContracts)
		p.POST("/persons/:id/contracts",                     persons.CreateContract)
		p.GET("/persons/:id/contracts/:contractId",          persons.GetContract)
		p.PUT("/persons/:id/contracts/:contractId",          persons.UpdateContract)
		p.DELETE("/persons/:id/contracts/:contractId",       persons.DeleteContract)
		// All contracts across every person
		p.GET("/contracts",                                  persons.ListAllContracts)
	}

	// ---- authenticated SOAP ----
	r.POST("/soap", auth.Middleware(), soap.Handler)

	// ---- profile mock routes (optional) ----
	if profile != nil {
		RegisterProfileRoutes(r, profile, auth, userLimiter, personLimiter, store, personStore, soap)
	}

	printBanner(cfg)
	r.Run(fmt.Sprintf(":%d", cfg.Port)) //nolint:errcheck
}

// resolveDataDir returns the data directory when storage type is "file",
// or an empty string for memory-only mode.
func resolveDataDir(cfg Config) string {
	if cfg.Storage.Type != "file" {
		return ""
	}
	if cfg.Storage.Path == "" {
		log.Fatal("config: storage.type is \"file\" but storage.path is empty")
	}
	return cfg.Storage.Path
}

func printBanner(cfg Config) {
	base := fmt.Sprintf("http://localhost:%d", cfg.Port)
	uRL := "disabled"
	if cfg.RateLimit.Users.RequestsPer10Seconds > 0 {
		uRL = fmt.Sprintf("%.0f req/10s (burst %d)", cfg.RateLimit.Users.RequestsPer10Seconds, cfg.RateLimit.Users.Burst)
	}
	pRL := "disabled"
	if cfg.RateLimit.Persons.RequestsPer10Seconds > 0 {
		pRL = fmt.Sprintf("%.0f req/10s (burst %d)", cfg.RateLimit.Persons.RequestsPer10Seconds, cfg.RateLimit.Persons.Burst)
	}
	fmt.Printf(`
┌──────────────────────────────────────────────────────────────────────┐
│                    go_testapi — API Test Server                       │
├──────────────────────────────────────────────────────────────────────┤
│  Listening on      %-49s│
│  Client ID         %-49s│
│  Secret            %-49s│
│  Users  rate limit %-49s│
│  Persons rate limit%-49s│
│  Users  page size  default=%-d max=%-d%*s│
│  Persons page size default=%-d max=%-d%*s│
├──────────────────┬───────────────────────────────────────────────────┤
│  GET             │  /docs                         API documentation   │
│  POST            │  /oauth/token                  get bearer token    │
│  GET             │  /health                       liveness check      │
│  GET             │  /soap                         WSDL document       │
│  POST            │  /soap                         SOAP endpoint       │
├──────────────────┼───────────────────────────────────────────────────┤
│  GET             │  /api/users                    list (paginated)    │
│  POST            │  /api/users                    create user         │
│  GET/PUT/DELETE  │  /api/users/:id                                    │
│  POST            │  /api/users/:id/enable|disable                     │
│  GET/POST        │  /api/users/:id/permissions                        │
│  DELETE          │  /api/users/:id/permissions/:perm                  │
├──────────────────┼───────────────────────────────────────────────────┤
│  GET             │  /api/persons                  list (paginated)    │
│  POST            │  /api/persons                  create person       │
│  GET/PUT/DELETE  │  /api/persons/:id                                  │
│  GET             │  /api/persons/:id/contracts    list (paginated)    │
│  POST            │  /api/persons/:id/contracts    create contract     │
│  GET/PUT/DELETE  │  /api/persons/:id/contracts/:contractId            │
│  GET             │  /api/contracts                all contracts       │
├──────────────────┴───────────────────────────────────────────────────┤
│  Auth:  Basic <client_id>:<secret>  OR  Bearer <token>               │
│  Page:  ?page=1&page_size=20                                         │
│  Fmt:   Accept: application/xml  OR  ?format=xml  for XML output     │
└──────────────────────────────────────────────────────────────────────┘
`,
		base,
		cfg.ClientID,
		cfg.Secret,
		uRL,
		pRL,
		cfg.Pagination.Users.DefaultPageSize, cfg.Pagination.Users.MaxPageSize,
		max(0, 34-countDigits(cfg.Pagination.Users.DefaultPageSize)-countDigits(cfg.Pagination.Users.MaxPageSize)), "│",
		cfg.Pagination.Persons.DefaultPageSize, cfg.Pagination.Persons.MaxPageSize,
		max(0, 34-countDigits(cfg.Pagination.Persons.DefaultPageSize)-countDigits(cfg.Pagination.Persons.MaxPageSize)), "│",
	)
}

func countDigits(n int) int {
	if n == 0 {
		return 1
	}
	count := 0
	for n != 0 {
		n /= 10
		count++
	}
	return count
}