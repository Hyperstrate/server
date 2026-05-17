package audit

import (
	"context"
	"net/http"
)

// Record is the payload passed to the global logger on each admin action.
type Record struct {
	OrgID      string
	UserEmail  string
	Action     string
	Resource   string
	ResourceID string
	Details    string
	IPAddress  string
}

// Logger is called synchronously after each successful admin mutation.
// Implementations must be fast (non-blocking) — write to a channel or a
// fire-and-forget repo call.
type Logger func(ctx context.Context, r Record)

var globalLogger Logger

// SetLogger registers the global audit logger. Call once at startup (from the
// observability module, just like webhook.SetRecorder).
func SetLogger(l Logger) { globalLogger = l }

// Log emits an audit record if a logger has been registered. It is a no-op
// if SetLogger has not been called.
func Log(ctx context.Context, r Record) {
	if globalLogger != nil {
		globalLogger(ctx, r)
	}
}

// IPFromRequest extracts the best-effort client IP from a Gin/HTTP request.
func IPFromRequest(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
