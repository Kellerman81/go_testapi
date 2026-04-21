package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const maxBodyLog = 64 * 1024 // 64 KB cap on logged body size

// capturingWriter wraps gin.ResponseWriter to record the bytes written.
type capturingWriter struct {
	gin.ResponseWriter
	buf *bytes.Buffer
}

func (w *capturingWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}

// SetupLogger opens (or creates) the log file at path and returns a logger
// that writes to it. The caller is responsible for closing the file on exit.
func SetupLogger(path string) (*log.Logger, *os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return log.New(f, "", 0), f, nil
}

// RequestLogger returns a Gin middleware that logs every request and its
// response to logger: timestamp, method, path, query, auth header, status,
// duration, request body, and response body.
func RequestLogger(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Snapshot request body so handlers can still read it.
		var reqBody []byte
		if c.Request.Body != nil && c.Request.Body != http.NoBody {
			reqBody, _ = io.ReadAll(io.LimitReader(c.Request.Body, maxBodyLog))
			c.Request.Body = io.NopCloser(bytes.NewBuffer(reqBody))
		}

		// Wrap the writer to capture the response body.
		cw := &capturingWriter{buf: &bytes.Buffer{}, ResponseWriter: c.Writer}
		c.Writer = cw

		c.Next()

		duration := time.Since(start)
		status := cw.Status()
		query := c.Request.URL.RawQuery

		var sb strings.Builder
		fmt.Fprintf(&sb, "\n%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(&sb, "%s  %s %s  →  %d %s  (%s)\n",
			time.Now().Format("2006-01-02 15:04:05.000"),
			c.Request.Method,
			c.Request.URL.Path,
			status,
			http.StatusText(status),
			duration.Round(time.Microsecond),
		)
		if query != "" {
			fmt.Fprintf(&sb, "Query:  %s\n", query)
		}
		if auth := c.GetHeader("Authorization"); auth != "" {
			fmt.Fprintf(&sb, "Auth:   %s\n", truncateAuth(auth))
		}
		if ct := c.GetHeader("Content-Type"); ct != "" {
			fmt.Fprintf(&sb, "C-Type: %s\n", ct)
		}
		if len(reqBody) > 0 {
			fmt.Fprintf(&sb, "%s\n", strings.Repeat("─", 40))
			fmt.Fprintf(&sb, "Request body:\n%s\n", prettyBody(reqBody))
		}
		if respBytes := cw.buf.Bytes(); len(respBytes) > 0 {
			fmt.Fprintf(&sb, "%s\n", strings.Repeat("─", 40))
			fmt.Fprintf(&sb, "Response body:\n%s\n", prettyBody(respBytes))
		}

		logger.Print(sb.String())
	}
}

// truncateAuth shortens a long auth header value to avoid logging full tokens.
func truncateAuth(s string) string {
	const keep = 20
	if len(s) <= keep+6 {
		return s
	}
	return s[:keep] + "…"
}

// prettyBody tries to return the body unchanged if it looks like JSON,
// capped at maxBodyLog bytes.
func prettyBody(b []byte) string {
	if len(b) > maxBodyLog {
		return string(b[:maxBodyLog]) + "\n… (truncated)"
	}
	return string(b)
}
