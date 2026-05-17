package application

import (
	"log/slog"
	"sync"
)

// RouterInferenceLoggedEvent is emitted after every routed inference call,
// both successful and failed. Source is always "router".
type RouterInferenceLoggedEvent struct {
	OrgID             string
	RouterID          string
	VirtualKeyID      string
	TeamID            string
	UserID            string
	ModelID           string
	SelectedTargetID  string
	ModelDefKey       string
	Provider          string
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CostUSD           float64
	LatencyMs         int64
	TTFTMs            int64  // time-to-first-token for streaming; 0 for sync calls
	Status            string // "success" | "error"
	ErrorMessage      string
	ABVariant         string // non-empty when routed by ab_test interceptor
	CacheHit          bool
	CacheHitType      string // "exact" | "semantic" | ""
	AgentSessionID    string
	Agent             string
	AgentRole         string
	ParentSessionID   string
	TurnIndex         int
	StorePayloads     bool
	RequestFields     string // JSON-encoded fields map (only set when StorePayloads=true)
	ResponseContent   string // model response text (only set when StorePayloads=true)
	PipelineSteps     []PipelineStep
	ToolCallCaptures  []ToolCallCapture
}

// RouterInferenceLoggedListener receives a RouterInferenceLoggedEvent.
type RouterInferenceLoggedListener func(e RouterInferenceLoggedEvent)

const (
	defaultRouterInferenceEventQueueSize = 1024
	defaultRouterInferenceEventWorkers   = 4
)

type routerInferenceEventDispatch struct {
	event     RouterInferenceLoggedEvent
	listeners []RouterInferenceLoggedListener
}

// RouterInferenceEventBus collects and fires router inference events.
// Register listeners with OnLogged; the router service emits via Emit.
type RouterInferenceEventBus struct {
	mu        sync.RWMutex
	listeners []RouterInferenceLoggedListener
	queue     chan routerInferenceEventDispatch
}

func NewRouterInferenceEventBus() *RouterInferenceEventBus {
	return NewRouterInferenceEventBusWithOptions(defaultRouterInferenceEventQueueSize, defaultRouterInferenceEventWorkers)
}

func NewRouterInferenceEventBusWithOptions(queueSize, workerCount int) *RouterInferenceEventBus {
	if queueSize <= 0 {
		queueSize = defaultRouterInferenceEventQueueSize
	}
	if workerCount <= 0 {
		workerCount = defaultRouterInferenceEventWorkers
	}
	b := &RouterInferenceEventBus{queue: make(chan routerInferenceEventDispatch, queueSize)}
	for i := 0; i < workerCount; i++ {
		go b.runWorker()
	}
	return b
}

// OnLogged registers a listener called after every completed routed inference call.
func (b *RouterInferenceEventBus) OnLogged(l RouterInferenceLoggedListener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, l)
}

// Emit queues all registered listeners. Events are fire-and-forget; the queue
// is bounded so slow listeners cannot create unbounded goroutines.
func (b *RouterInferenceEventBus) Emit(e RouterInferenceLoggedEvent) {
	b.mu.RLock()
	listeners := append([]RouterInferenceLoggedListener(nil), b.listeners...)
	b.mu.RUnlock()
	if len(listeners) == 0 {
		return
	}
	dispatch := routerInferenceEventDispatch{event: e, listeners: listeners}
	select {
	case b.queue <- dispatch:
	default:
		slog.Warn("router inference event dropped", "routerID", e.RouterID, "status", e.Status)
	}
}

func (b *RouterInferenceEventBus) runWorker() {
	for dispatch := range b.queue {
		for _, listener := range dispatch.listeners {
			callRouterInferenceListener(listener, dispatch.event)
		}
	}
}

func callRouterInferenceListener(listener RouterInferenceLoggedListener, event RouterInferenceLoggedEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("router inference event listener panic", "panic", r)
		}
	}()
	listener(event)
}

// ── Target lifecycle events ───────────────────────────────────────────────────

// TargetDeletedEvent is emitted after a router target is permanently removed.
type TargetDeletedEvent struct {
	OrgID    string
	RouterID string
	TargetID string
	ModelID  string // model registration ID the target pointed at
}

// TargetDeletedListener handles a TargetDeletedEvent.
type TargetDeletedListener func(e TargetDeletedEvent)

// RouterTargetEventBus collects and fires target lifecycle events.
type RouterTargetEventBus struct {
	onDeleted []TargetDeletedListener
}

func NewRouterTargetEventBus() *RouterTargetEventBus {
	return &RouterTargetEventBus{}
}

// OnDeleted registers a listener called whenever a target is deleted.
func (b *RouterTargetEventBus) OnDeleted(l TargetDeletedListener) {
	b.onDeleted = append(b.onDeleted, l)
}

// EmitDeleted fires all registered OnDeleted listeners in registration order.
func (b *RouterTargetEventBus) EmitDeleted(e TargetDeletedEvent) {
	for _, l := range b.onDeleted {
		l(e)
	}
}
