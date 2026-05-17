package application

import (
	"context"
	"errors"
	"log/slog"
)

// PromptDeletedEvent is emitted after a prompt is permanently removed.
type PromptDeletedEvent struct {
	PromptID string
}

// PromptDeletedListener handles a PromptDeletedEvent.
// Errors are logged but do not abort the deletion — all listeners always run.
type PromptDeletedListener func(ctx context.Context, e PromptDeletedEvent) error

// PromptEventBus collects and fires prompt lifecycle events.
// Register listeners with OnDeleted; emit with EmitDeleted.
type PromptEventBus struct {
	onDeleted []PromptDeletedListener
}

func NewPromptEventBus() *PromptEventBus {
	return &PromptEventBus{}
}

// OnDeleted registers a listener that is called whenever a prompt is deleted.
func (b *PromptEventBus) OnDeleted(l PromptDeletedListener) {
	b.onDeleted = append(b.onDeleted, l)
}

// EmitDeleted fires all registered OnDeleted listeners in registration order.
// All listeners run even when some fail; a combined error is returned if any did.
func (b *PromptEventBus) EmitDeleted(ctx context.Context, e PromptDeletedEvent) error {
	var errs []error
	for _, l := range b.onDeleted {
		if err := l(ctx, e); err != nil {
			slog.Error("PromptEventBus OnDeleted listener error", "promptID", e.PromptID, "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
