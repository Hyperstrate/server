package application

import (
	"context"
	"errors"
	"log/slog"
)

// ── Model lifecycle events ────────────────────────────────────────────────────

// ModelDeletedEvent is emitted after a model registration is permanently removed.
type ModelDeletedEvent struct {
	ModelID string
	OrgID   string
}

// ModelDeletedListener handles a ModelDeletedEvent.
// Errors are logged but do not abort the deletion — all listeners always run.
type ModelDeletedListener func(ctx context.Context, e ModelDeletedEvent) error

// ModelEventBus collects and fires model lifecycle events.
// Register listeners with OnDeleted; emit with EmitDeleted.
type ModelEventBus struct {
	onDeleted []ModelDeletedListener
}

func NewModelEventBus() *ModelEventBus {
	return &ModelEventBus{}
}

// OnDeleted registers a listener that is called whenever a model is deleted.
func (b *ModelEventBus) OnDeleted(l ModelDeletedListener) {
	b.onDeleted = append(b.onDeleted, l)
}

// EmitDeleted fires all registered OnDeleted listeners in registration order.
// All listeners run even when some fail; a combined error is returned if any did.
func (b *ModelEventBus) EmitDeleted(ctx context.Context, e ModelDeletedEvent) error {
	var errs []error
	for _, l := range b.onDeleted {
		if err := l(ctx, e); err != nil {
			slog.Error("ModelEventBus OnDeleted listener error", "modelID", e.ModelID, "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ── Inference events ──────────────────────────────────────────────────────────

// InferenceLoggedEvent is emitted after direct or async-job inference calls,
// both successful and failed. Router calls use RouterInferenceLoggedEvent.
type InferenceLoggedEvent struct {
	OrgID        string
	ModelID      string
	ModelDefKey  string
	Provider     string
	InputTokens  int64
	OutputTokens int64
	// CachedInputTokens counts input tokens served from provider-side prompt cache.
	CachedInputTokens int64
	CostUSD           float64
	LatencyMs         int64
	Status            string // "success" | "error"
	ErrorMessage      string
	Source            string // "direct" | "job"
}

// InferenceLoggedListener receives an InferenceLoggedEvent.
type InferenceLoggedListener func(e InferenceLoggedEvent)

// InferenceEventBus collects and fires inference events.
// Register listeners with OnLogged; the AI service emits via Emit.
type InferenceEventBus struct {
	listeners []InferenceLoggedListener
}

func NewInferenceEventBus() *InferenceEventBus { return &InferenceEventBus{} }

// OnLogged registers a listener called after every completed inference call.
func (b *InferenceEventBus) OnLogged(l InferenceLoggedListener) {
	b.listeners = append(b.listeners, l)
}

// Emit fires all registered listeners. Listeners run fire-and-forget;
// they must not block the caller.
func (b *InferenceEventBus) Emit(e InferenceLoggedEvent) {
	for _, l := range b.listeners {
		l(e)
	}
}

// ── MCP server lifecycle events ───────────────────────────────────────────────

// MCPServerDeletedEvent is emitted after a managed MCP server is permanently removed.
type MCPServerDeletedEvent struct {
	OrgID    string
	ServerID string
}

// MCPServerDeletedListener handles an MCPServerDeletedEvent.
type MCPServerDeletedListener func(e MCPServerDeletedEvent)

// MCPServerEventBus collects and fires MCP server lifecycle events.
type MCPServerEventBus struct {
	onDeleted []MCPServerDeletedListener
}

func NewMCPServerEventBus() *MCPServerEventBus {
	return &MCPServerEventBus{}
}

// OnDeleted registers a listener called whenever an MCP server is deleted.
func (b *MCPServerEventBus) OnDeleted(l MCPServerDeletedListener) {
	b.onDeleted = append(b.onDeleted, l)
}

// EmitDeleted fires all registered OnDeleted listeners in registration order.
func (b *MCPServerEventBus) EmitDeleted(e MCPServerDeletedEvent) {
	for _, l := range b.onDeleted {
		l(e)
	}
}
