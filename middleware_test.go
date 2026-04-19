package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	// Silence Gin's startup output in tests.
	gin.SetMode(gin.TestMode)
}

// ---- helpers ----

// limiterRouter returns a minimal Gin router with the given rate-limit config
// applied. Every GET /test that passes the limiter responds 200.
func limiterRouter(cfg RateLimitGroupConfig) *gin.Engine {
	r := gin.New()
	lim := NewResourceLimiter(cfg)
	r.GET("/test", lim.Middleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

// get fires a GET /test request and returns the HTTP status code.
func get(r *gin.Engine) int {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
	return w.Code
}

// getN fires n sequential GET /test requests and returns all status codes.
func getN(r *gin.Engine, n int) []int {
	codes := make([]int, n)
	for i := range codes {
		codes[i] = get(r)
	}
	return codes
}

// ============================================================
// Rate-limiter tests
// ============================================================

func TestRateLimiter_Disabled_AllRequestsPass(t *testing.T) {
	// RequestsPer10Seconds = 0 → disabled; every request must get through.
	r := limiterRouter(RateLimitGroupConfig{RequestsPer10Seconds: 0, Burst: 1})
	for i, code := range getN(r, 30) {
		if code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d (limiter should be off)", i+1, code)
		}
	}
}

func TestRateLimiter_Negative_AllRequestsPass(t *testing.T) {
	r := limiterRouter(RateLimitGroupConfig{RequestsPer10Seconds: -5, Burst: 2})
	for i, code := range getN(r, 10) {
		if code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, code)
		}
	}
}

func TestRateLimiter_BurstAllowed(t *testing.T) {
	// burst=3 → first 3 requests must all succeed regardless of the rate.
	r := limiterRouter(RateLimitGroupConfig{RequestsPer10Seconds: 1, Burst: 3})
	for i, code := range getN(r, 3) {
		if code != http.StatusOK {
			t.Fatalf("request %d within burst: expected 200, got %d", i+1, code)
		}
	}
}

func TestRateLimiter_ExceedsBurst_Returns429(t *testing.T) {
	// burst=2, slow rate → 3rd and subsequent requests must be rejected.
	r := limiterRouter(RateLimitGroupConfig{RequestsPer10Seconds: 1, Burst: 2})
	codes := getN(r, 5)

	for i, code := range codes {
		if i < 2 {
			if code != http.StatusOK {
				t.Errorf("request %d (within burst): expected 200, got %d", i+1, code)
			}
		} else {
			if code != http.StatusTooManyRequests {
				t.Errorf("request %d (over burst): expected 429, got %d", i+1, code)
			}
		}
	}
}

func TestRateLimiter_Returns429_WithRetryAfterHeader(t *testing.T) {
	// burst=1 → second request is always rejected.
	r := limiterRouter(RateLimitGroupConfig{RequestsPer10Seconds: 1, Burst: 1})
	get(r) // consume the one burst token

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if h := w.Header().Get("Retry-After"); h != "1" {
		t.Errorf("expected Retry-After: 1, got %q", h)
	}
}

func TestRateLimiter_Returns429_WithCorrectBody(t *testing.T) {
	r := limiterRouter(RateLimitGroupConfig{RequestsPer10Seconds: 1, Burst: 1})
	get(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))

	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Message == "" {
		t.Error("expected a non-empty message in the 429 body")
	}
}

func TestRateLimiter_TokensRefillAfterWait(t *testing.T) {
	// 10 req / 10 s = 1 req/s → one new token every second.
	// burst=1 so we can observe the refill precisely.
	r := limiterRouter(RateLimitGroupConfig{RequestsPer10Seconds: 10, Burst: 1})

	if code := get(r); code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", code)
	}
	if code := get(r); code != http.StatusTooManyRequests {
		t.Fatalf("immediate follow-up: expected 429, got %d", code)
	}

	// Wait slightly longer than 1 token-refill interval (1 s).
	time.Sleep(1100 * time.Millisecond)

	if code := get(r); code != http.StatusOK {
		t.Fatalf("request after refill wait: expected 200, got %d", code)
	}
}

func TestRateLimiter_BurstOf1_OnlyFirstPasses(t *testing.T) {
	r := limiterRouter(RateLimitGroupConfig{RequestsPer10Seconds: 1, Burst: 1})
	codes := getN(r, 4)
	if codes[0] != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", codes[0])
	}
	for i := 1; i < len(codes); i++ {
		if codes[i] != http.StatusTooManyRequests {
			t.Errorf("request %d: expected 429, got %d", i+1, codes[i])
		}
	}
}

// ============================================================
// Pagination validation tests
// ============================================================

// pageRouter wraps parsePage in a test endpoint that echoes the resolved
// page / page_size back as JSON, or aborts with the 400 that parsePage wrote.
func pageRouter(cfg PaginationGroupConfig) *gin.Engine {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		page, pageSize, ok := parsePage(c, cfg)
		if !ok {
			return
		}
		c.JSON(http.StatusOK, gin.H{"page": page, "page_size": pageSize})
	})
	return r
}

type pageResponse struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

func parsedPage(r *gin.Engine, query string) (int, pageResponse) {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test?"+query, nil))
	var pr pageResponse
	json.NewDecoder(w.Body).Decode(&pr)
	return w.Code, pr
}

func TestParsePage_Defaults(t *testing.T) {
	cfg := PaginationGroupConfig{DefaultPageSize: 15, MaxPageSize: 50}
	r := pageRouter(cfg)
	code, pr := parsedPage(r, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if pr.Page != 1 {
		t.Errorf("page: got %d, want 1", pr.Page)
	}
	if pr.PageSize != 15 {
		t.Errorf("page_size: got %d, want 15 (default)", pr.PageSize)
	}
}

func TestParsePage_ValidValues(t *testing.T) {
	cfg := PaginationGroupConfig{DefaultPageSize: 10, MaxPageSize: 100}
	r := pageRouter(cfg)
	code, pr := parsedPage(r, "page=3&page_size=25")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if pr.Page != 3 {
		t.Errorf("page: got %d, want 3", pr.Page)
	}
	if pr.PageSize != 25 {
		t.Errorf("page_size: got %d, want 25", pr.PageSize)
	}
}

func TestParsePage_PageZero_Returns400(t *testing.T) {
	r := pageRouter(PaginationGroupConfig{DefaultPageSize: 10, MaxPageSize: 100})
	code, _ := parsedPage(r, "page=0")
	if code != http.StatusBadRequest {
		t.Errorf("page=0: expected 400, got %d", code)
	}
}

func TestParsePage_PageNegative_Returns400(t *testing.T) {
	r := pageRouter(PaginationGroupConfig{DefaultPageSize: 10, MaxPageSize: 100})
	code, _ := parsedPage(r, "page=-1")
	if code != http.StatusBadRequest {
		t.Errorf("page=-1: expected 400, got %d", code)
	}
}

func TestParsePage_PageNonNumeric_Returns400(t *testing.T) {
	r := pageRouter(PaginationGroupConfig{DefaultPageSize: 10, MaxPageSize: 100})
	code, _ := parsedPage(r, "page=abc")
	if code != http.StatusBadRequest {
		t.Errorf("page=abc: expected 400, got %d", code)
	}
}

func TestParsePage_PageSizeZero_Returns400(t *testing.T) {
	r := pageRouter(PaginationGroupConfig{DefaultPageSize: 10, MaxPageSize: 100})
	code, _ := parsedPage(r, "page_size=0")
	if code != http.StatusBadRequest {
		t.Errorf("page_size=0: expected 400, got %d", code)
	}
}

func TestParsePage_PageSizeAboveMax_IsClamped(t *testing.T) {
	cfg := PaginationGroupConfig{DefaultPageSize: 10, MaxPageSize: 50}
	r := pageRouter(cfg)
	code, pr := parsedPage(r, "page_size=999")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if pr.PageSize != 50 {
		t.Errorf("page_size: got %d, want 50 (clamped to max)", pr.PageSize)
	}
}

func TestParsePage_400ErrorBodyHasMessage(t *testing.T) {
	r := pageRouter(PaginationGroupConfig{DefaultPageSize: 10, MaxPageSize: 100})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test?page=0", nil))

	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode 400 body: %v", err)
	}
	if body.Message == "" {
		t.Error("expected a non-empty message in the 400 body")
	}
}
