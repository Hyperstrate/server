package application

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRouterInferenceEventBus_callsAllListeners(t *testing.T) {
	bus := NewRouterInferenceEventBus()

	var got1, got2 RouterInferenceLoggedEvent
	var wg sync.WaitGroup
	wg.Add(2)
	bus.OnLogged(func(e RouterInferenceLoggedEvent) { got1 = e; wg.Done() })
	bus.OnLogged(func(e RouterInferenceLoggedEvent) { got2 = e; wg.Done() })

	bus.Emit(RouterInferenceLoggedEvent{
		OrgID: "org1", RouterID: "rtr1", ModelID: "mdl1",
		LatencyMs: 250, Status: "success",
	})
	waitForEventBus(t, &wg)

	if got1.OrgID != "org1" || got1.RouterID != "rtr1" || got1.Status != "success" {
		t.Errorf("listener 1 got wrong event: %+v", got1)
	}
	if got2.OrgID != "org1" || got2.RouterID != "rtr1" || got2.Status != "success" {
		t.Errorf("listener 2 got wrong event: %+v", got2)
	}
}

func TestRouterInferenceEventBus_noListenersEmitIsNoOp(t *testing.T) {
	bus := NewRouterInferenceEventBus()
	// Should not panic with no listeners registered.
	bus.Emit(RouterInferenceLoggedEvent{OrgID: "org1", Status: "success"})
}

func TestRouterInferenceEventBus_errorEventCarriesMessage(t *testing.T) {
	bus := NewRouterInferenceEventBus()

	var got RouterInferenceLoggedEvent
	var wg sync.WaitGroup
	wg.Add(1)
	bus.OnLogged(func(e RouterInferenceLoggedEvent) { got = e; wg.Done() })

	bus.Emit(RouterInferenceLoggedEvent{
		OrgID: "org1", RouterID: "rtr1", Status: "error",
		ErrorMessage: "upstream timeout",
	})
	waitForEventBus(t, &wg)

	if got.Status != "error" || got.ErrorMessage != "upstream timeout" {
		t.Errorf("want error event with message, got %+v", got)
	}
}

func TestRouterInferenceEventBus_emitDoesNotWaitForListener(t *testing.T) {
	bus := NewRouterInferenceEventBus()
	release := make(chan struct{})
	started := make(chan struct{})
	bus.OnLogged(func(_ RouterInferenceLoggedEvent) {
		close(started)
		<-release
	})

	done := make(chan struct{})
	go func() {
		bus.Emit(RouterInferenceLoggedEvent{Status: "success"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Emit blocked on listener")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("listener did not start")
	}
	close(release)
}

func TestRouterInferenceEventBus_dropsWhenWorkerQueueFull(t *testing.T) {
	bus := NewRouterInferenceEventBusWithOptions(1, 1)
	release := make(chan struct{})
	started := make(chan struct{})
	var processed atomic.Int64

	bus.OnLogged(func(e RouterInferenceLoggedEvent) {
		processed.Add(1)
		if e.RouterID == "blocking" {
			close(started)
			<-release
		}
	})

	bus.Emit(RouterInferenceLoggedEvent{RouterID: "blocking", Status: "success"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("blocking listener did not start")
	}

	bus.Emit(RouterInferenceLoggedEvent{RouterID: "queued", Status: "success"})
	bus.Emit(RouterInferenceLoggedEvent{RouterID: "dropped", Status: "success"})
	close(release)

	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if processed.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)

	if got := processed.Load(); got != 2 {
		t.Fatalf("processed events = %d, want 2 with one dropped event", got)
	}
}

func TestRouterInferenceEventBus_listenerPanicDoesNotStopLaterListeners(t *testing.T) {
	bus := NewRouterInferenceEventBus()
	var wg sync.WaitGroup
	wg.Add(1)
	bus.OnLogged(func(_ RouterInferenceLoggedEvent) { panic("boom") })
	bus.OnLogged(func(_ RouterInferenceLoggedEvent) { wg.Done() })

	bus.Emit(RouterInferenceLoggedEvent{Status: "success"})

	waitForEventBus(t, &wg)
}

func TestRouterInferenceEventBus_listenersReceivedInRegistrationOrder(t *testing.T) {
	bus := NewRouterInferenceEventBus()

	order := make([]int, 0, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	bus.OnLogged(func(_ RouterInferenceLoggedEvent) { order = append(order, 1); wg.Done() })
	bus.OnLogged(func(_ RouterInferenceLoggedEvent) { order = append(order, 2); wg.Done() })
	bus.OnLogged(func(_ RouterInferenceLoggedEvent) { order = append(order, 3); wg.Done() })

	bus.Emit(RouterInferenceLoggedEvent{Status: "success"})
	waitForEventBus(t, &wg)

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("want listeners fired in order [1 2 3], got %v", order)
	}
}

func waitForEventBus(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event listeners")
	}
}
