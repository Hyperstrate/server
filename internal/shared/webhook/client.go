package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Event is the envelope sent to webhook URLs.
type Event struct {
	Event    string `json:"event"`
	RouterID string `json:"routerId,omitempty"`
	Detail   string `json:"detail,omitempty"`
	// BudgetUsedPercent is set on budget threshold events.
	BudgetUsedPercent float64 `json:"budgetUsedPercent,omitempty"`
	// Timestamp is the RFC3339 time the event was fired.
	Timestamp string `json:"timestamp"`
}

const (
	EventBudgetExceeded    = "budget_exceeded"
	EventBudgetThreshold   = "budget_threshold"
	EventAllTargetsFailed  = "all_targets_failed"
	EventRateLimitExceeded = "rate_limit_exceeded"
	EventQualityGateFailed = "quality_gate_failed"
	EventRequestBlocked    = "request_blocked"
	EventLoopDetected      = "loop_detected"
)

// DeliveryRecord is populated after each delivery attempt and passed to Recorder.
type DeliveryRecord struct {
	RouterID   string
	Event      string
	URL        string
	StatusCode int
	Success    bool
	ErrorMsg   string
	CreatedAt  time.Time
}

// Recorder is called after every webhook delivery attempt (success or failure).
// The call happens in the same goroutine as the HTTP request, so it must be fast
// (e.g. write to a channel or call a non-blocking repo).
type Recorder func(r DeliveryRecord)

var globalRecorder Recorder

// SetRecorder registers a global delivery recorder. Call once at startup.
func SetRecorder(r Recorder) { globalRecorder = r }

// validateWebhookURL rejects URLs that point to loopback, private, or
// link-local addresses to prevent SSRF. Only http/https are allowed.
func validateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook URL must use http or https, got %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	// Block by literal hostname first (fast path, no DNS).
	blocked := []string{"localhost", "127.0.0.1", "::1", "0.0.0.0", "169.254.169.254"}
	for _, b := range blocked {
		if host == b {
			return fmt.Errorf("webhook URL targets a blocked address %q", host)
		}
	}
	// If host is a bare IP, check address class.
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("webhook URL must not target a private or local address")
		}
	}
	return nil
}

// Fire sends a POST to url with payload as JSON. It is non-blocking: the
// HTTP call runs in its own goroutine so the caller is never delayed.
// Delivery failures are logged but not surfaced to the caller.
func Fire(rawURL string, event Event) {
	if rawURL == "" {
		return
	}
	if err := validateWebhookURL(rawURL); err != nil {
		slog.Error("webhook URL rejected", "url", rawURL, "err", err)
		return
	}
	event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		body, err := json.Marshal(event)
		if err != nil {
			slog.Error("webhook marshal failed", "url", rawURL, "err", err)
			record(event.RouterID, event.Event, rawURL, 0, false, err.Error())
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
		if err != nil {
			slog.Error("webhook build request failed", "url", rawURL, "err", err)
			record(event.RouterID, event.Event, rawURL, 0, false, err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Hyperstrate-Webhook/1.0")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Error("webhook delivery failed", "url", rawURL, "err", err)
			record(event.RouterID, event.Event, rawURL, 0, false, err.Error())
			return
		}
		resp.Body.Close()
		success := resp.StatusCode < 400
		if !success {
			slog.Warn("webhook returned non-2xx", "url", rawURL, "status", resp.StatusCode)
		}
		record(event.RouterID, event.Event, rawURL, resp.StatusCode, success, "")
	}()
}

func record(routerID, eventName, url string, code int, ok bool, errMsg string) {
	if globalRecorder == nil {
		return
	}
	globalRecorder(DeliveryRecord{
		RouterID:   routerID,
		Event:      eventName,
		URL:        url,
		StatusCode: code,
		Success:    ok,
		ErrorMsg:   errMsg,
		CreatedAt:  time.Now().UTC(),
	})
}
