package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// ============================================================
// Rate limiting
// ============================================================

// ResourceLimiter is a token-bucket rate limiter for one resource group.
type ResourceLimiter struct {
	limiter *rate.Limiter // nil when limiting is disabled
}

// NewResourceLimiter builds a limiter from config.
// RequestsPer10Seconds <= 0 disables limiting (all requests pass through).
func NewResourceLimiter(cfg RateLimitGroupConfig) *ResourceLimiter {
	if cfg.RequestsPer10Seconds <= 0 {
		return &ResourceLimiter{}
	}
	burst := cfg.Burst
	if burst < 1 {
		burst = 1
	}
	// Convert to per-second rate for the token bucket.
	ratePerSec := cfg.RequestsPer10Seconds / 10.0
	return &ResourceLimiter{
		limiter: rate.NewLimiter(rate.Limit(ratePerSec), burst),
	}
}

// Middleware returns a Gin handler that enforces the rate limit.
// When the limit is exceeded the handler returns 429 with a Retry-After header.
func (rl *ResourceLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if rl.limiter != nil && !rl.limiter.Allow() {
			c.Header("Retry-After", "1")
			respond(c, http.StatusTooManyRequests, apiMessage{
				Message: "rate limit exceeded — try again later",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ============================================================
// Pagination
// ============================================================

// PageMeta carries pagination metadata returned alongside every list response.
type PageMeta struct {
	Page       int    `json:"page"        xml:"Page"`
	PageSize   int    `json:"page_size"   xml:"PageSize"`
	Total      int    `json:"total"       xml:"Total"`
	TotalPages int    `json:"total_pages" xml:"TotalPages"`
	NextPage   string `json:"next_page"   xml:"NextPage"`
}

// parsePage reads ?page and ?page_size from the request.
//
// Rules:
//   - If the param is absent → use the configured default.
//   - If the param is present but not a positive integer → abort with 400.
//   - page_size is silently clamped to MaxPageSize (no error, just enforced).
//
// Returns (0, 0, false) and writes a 400 response when validation fails.
// Callers must return immediately when ok == false.
func parsePage(c *gin.Context, cfg PaginationGroupConfig) (page, pageSize int, ok bool) {
	var err error

	page, err = parsePositiveIntParam(c, "page", 1)
	if err != nil {
		respondMsg(c, http.StatusBadRequest, err.Error())
		c.Abort()
		return 0, 0, false
	}

	pageSize, err = parsePositiveIntParam(c, "page_size", cfg.DefaultPageSize)
	if err != nil {
		respondMsg(c, http.StatusBadRequest, err.Error())
		c.Abort()
		return 0, 0, false
	}
	if pageSize > cfg.MaxPageSize {
		pageSize = cfg.MaxPageSize
	}

	return page, pageSize, true
}

// parsePositiveIntParam parses a query parameter as a positive integer.
// Returns def when the parameter is absent.
// Returns an error when the parameter is present but <= 0 or not a number.
func parsePositiveIntParam(c *gin.Context, key string, def int) (int, error) {
	raw := c.Query(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer ≥ 1 (got %q)", key, raw)
	}
	return n, nil
}

// paginate slices items and computes metadata. Works with any type via generics.
// NextPage is left empty here; callers set it via nextPageURL after receiving the context.
func paginate[T any](items []T, page, pageSize int) ([]T, PageMeta) {
	total := len(items)
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}

	start := (page - 1) * pageSize
	if start >= total {
		meta := PageMeta{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}
		return []T{}, meta
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	slice := items[start:end]
	meta := PageMeta{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}
	return slice, meta
}

// nextPageURL returns a relative URL for the next page, preserving all existing
// query parameters. Returns an empty string when already on the last page.
func nextPageURL(c *gin.Context, page, totalPages int) string {
	if page >= totalPages {
		return ""
	}
	q := c.Request.URL.Query()
	q.Set("page", strconv.Itoa(page+1))
	return c.Request.URL.Path + "?" + q.Encode()
}
