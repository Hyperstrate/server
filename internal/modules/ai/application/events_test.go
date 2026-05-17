package application

import (
	"context"
	"errors"
	"testing"
)

func TestInferenceEventBus_callsAllListeners(t *testing.T) {
	bus := NewInferenceEventBus()

	var got1, got2 InferenceLoggedEvent
	bus.OnLogged(func(e InferenceLoggedEvent) { got1 = e })
	bus.OnLogged(func(e InferenceLoggedEvent) { got2 = e })

	bus.Emit(InferenceLoggedEvent{
		OrgID: "org1", ModelID: "mdl1", ModelDefKey: "openai/gpt-4o",
		Provider: "openai", InputTokens: 100, OutputTokens: 50,
		CostUSD: 0.002, LatencyMs: 300, Status: "success", Source: "direct",
	})

	for i, got := range []InferenceLoggedEvent{got1, got2} {
		if got.OrgID != "org1" || got.ModelID != "mdl1" || got.Status != "success" {
			t.Errorf("listener %d got wrong event: %+v", i+1, got)
		}
		if got.InputTokens != 100 || got.OutputTokens != 50 {
			t.Errorf("listener %d wrong token counts: in=%d out=%d", i+1, got.InputTokens, got.OutputTokens)
		}
		if got.Source != "direct" {
			t.Errorf("listener %d wrong source: %q", i+1, got.Source)
		}
	}
}

func TestInferenceEventBus_noListenersEmitIsNoOp(t *testing.T) {
	bus := NewInferenceEventBus()
	// Must not panic.
	bus.Emit(InferenceLoggedEvent{OrgID: "org1", Status: "success"})
}

func TestInferenceEventBus_errorEventCarriesMessage(t *testing.T) {
	bus := NewInferenceEventBus()

	var got InferenceLoggedEvent
	bus.OnLogged(func(e InferenceLoggedEvent) { got = e })

	bus.Emit(InferenceLoggedEvent{
		OrgID: "org1", ModelID: "mdl1",
		Status: "error", ErrorMessage: "upstream returned 429", Source: "direct",
	})

	if got.Status != "error" || got.ErrorMessage != "upstream returned 429" {
		t.Errorf("want error event propagated, got %+v", got)
	}
}

func TestInferenceEventBus_listenersCalledInRegistrationOrder(t *testing.T) {
	bus := NewInferenceEventBus()

	order := make([]int, 0, 3)
	bus.OnLogged(func(_ InferenceLoggedEvent) { order = append(order, 1) })
	bus.OnLogged(func(_ InferenceLoggedEvent) { order = append(order, 2) })
	bus.OnLogged(func(_ InferenceLoggedEvent) { order = append(order, 3) })

	bus.Emit(InferenceLoggedEvent{Status: "success"})

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("want listeners fired in order [1 2 3], got %v", order)
	}
}

func TestModelEventBus_onDeletedCallsAllListeners(t *testing.T) {
	bus := NewModelEventBus()

	var ids []string
	bus.OnDeleted(func(_ context.Context, e ModelDeletedEvent) error {
		ids = append(ids, "a:"+e.ModelID)
		return nil
	})
	bus.OnDeleted(func(_ context.Context, e ModelDeletedEvent) error {
		ids = append(ids, "b:"+e.ModelID)
		return nil
	})

	if err := bus.EmitDeleted(context.Background(), ModelDeletedEvent{ModelID: "mdl_1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ids) != 2 || ids[0] != "a:mdl_1" || ids[1] != "b:mdl_1" {
		t.Errorf("want [a:mdl_1 b:mdl_1], got %v", ids)
	}
}

func TestModelEventBus_allListenersRunEvenIfOneFails(t *testing.T) {
	bus := NewModelEventBus()

	secondRan := false
	bus.OnDeleted(func(_ context.Context, _ ModelDeletedEvent) error {
		return errors.New("listener 1 failed")
	})
	bus.OnDeleted(func(_ context.Context, _ ModelDeletedEvent) error {
		secondRan = true
		return nil
	})

	err := bus.EmitDeleted(context.Background(), ModelDeletedEvent{ModelID: "mdl_1"})
	if err == nil {
		t.Error("want error from failing listener, got nil")
	}
	if !secondRan {
		t.Error("want second listener to run even when first failed")
	}
}
