package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"hyperstrate/server/internal/modules/observability/domain"
)

// HealthMonitor periodically pings each registered model's provider and records
// the result in provider_health. The recorded latencies feed latency-based routing.
type HealthMonitor struct {
	svc        Service
	getModels  func(ctx context.Context) ([]ModelHealthTarget, error)
	interval   time.Duration
	httpClient *http.Client
}

// ModelHealthTarget carries enough info to probe a model's provider.
type ModelHealthTarget struct {
	ModelID     string
	ModelDefKey string
	Provider    string
	BaseURL     string
	APIKey      string
}

func NewHealthMonitor(svc Service, getModels func(ctx context.Context) ([]ModelHealthTarget, error), interval time.Duration) *HealthMonitor {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	return &HealthMonitor{
		svc:        svc,
		getModels:  getModels,
		interval:   interval,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Start runs the health-check loop in the background.
// Call with a cancellable context; returns when ctx is done.
func (m *HealthMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Run immediately on start, then on each tick.
	m.runChecks(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runChecks(ctx)
		}
	}
}

func (m *HealthMonitor) runChecks(ctx context.Context) {
	targets, err := m.getModels(ctx)
	if err != nil {
		slog.Error("get models", "err", err)
		return
	}
	for _, t := range targets {
		select {
		case <-ctx.Done():
			return
		default:
		}
		m.checkOne(t)
	}
}

func (m *HealthMonitor) checkOne(t ModelHealthTarget) {
	start := time.Now()
	url, body, headers := probeURL(t)
	if url == "" {
		return
	}

	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		m.record(t, false, 0, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := m.httpClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		m.record(t, false, latency, err.Error())
		return
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	resp.Body.Close()

	healthy := resp.StatusCode < 500
	errMsg := ""
	if !healthy {
		errMsg = fmt.Sprintf("upstream returned %d", resp.StatusCode)
	}
	m.record(t, healthy, latency, errMsg)
}

func (m *HealthMonitor) record(t ModelHealthTarget, healthy bool, latencyMs int64, errMsg string) {
	h := &domain.ProviderHealth{
		ModelID:      t.ModelID,
		ModelDefKey:  t.ModelDefKey,
		Provider:     t.Provider,
		IsHealthy:    healthy,
		LatencyMs:    latencyMs,
		ErrorMessage: errMsg,
		CheckedAt:    time.Now(),
	}
	if err := m.svc.UpsertProviderHealth(h); err != nil {
		slog.Error("upsert model", "modelID", t.ModelID, "err", err)
	}
}

// probeURL returns a minimal valid request for the provider.
// Returns ("", nil, nil) for providers that can't be pinged without credentials.
func probeURL(t ModelHealthTarget) (url string, body map[string]any, headers map[string]string) {
	if t.BaseURL == "" {
		return "", nil, nil
	}
	switch t.Provider {
	case "openai", "mistral":
		return t.BaseURL + "/v1/models",
			nil,
			map[string]string{"Authorization": "Bearer " + t.APIKey}
	case "anthropic":
		return "", nil, nil // Anthropic has no cheap ping endpoint; skip
	case "ollama":
		return t.BaseURL + "/api/tags", nil, nil
	case "gemini":
		return fmt.Sprintf("%s/v1beta/models?key=%s", t.BaseURL, t.APIKey), nil, nil
	default:
		return "", nil, nil
	}
}
