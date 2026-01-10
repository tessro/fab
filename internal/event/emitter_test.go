package event

import (
	"sync"
	"sync/atomic"
	"testing"
)

type testEvent struct {
	Value int
}

func TestEmitter_OnEvent(t *testing.T) {
	var e Emitter[testEvent]

	var received []testEvent
	e.OnEvent(func(ev testEvent) {
		received = append(received, ev)
	})

	e.Emit(testEvent{Value: 42})

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Value != 42 {
		t.Errorf("expected value 42, got %d", received[0].Value)
	}
}

func TestEmitter_MultipleHandlers(t *testing.T) {
	var e Emitter[testEvent]

	var count1, count2 int
	e.OnEvent(func(_ testEvent) {
		count1++
	})
	e.OnEvent(func(_ testEvent) {
		count2++
	})

	e.Emit(testEvent{Value: 1})

	if count1 != 1 {
		t.Errorf("handler 1 expected 1 call, got %d", count1)
	}
	if count2 != 1 {
		t.Errorf("handler 2 expected 1 call, got %d", count2)
	}
}

func TestEmitter_EmitToNoHandlers(t *testing.T) {
	var e Emitter[testEvent]

	// Should not panic when emitting with no handlers
	e.Emit(testEvent{Value: 42})
}

func TestEmitter_HandlerCopyBehavior(t *testing.T) {
	var e Emitter[testEvent]

	var callOrder []int
	e.OnEvent(func(_ testEvent) {
		callOrder = append(callOrder, 1)
		// Register a new handler during emission
		e.OnEvent(func(_ testEvent) {
			callOrder = append(callOrder, 3)
		})
	})
	e.OnEvent(func(_ testEvent) {
		callOrder = append(callOrder, 2)
	})

	e.Emit(testEvent{Value: 1})

	// Only handlers 1 and 2 should be called, not 3 (added during emission)
	if len(callOrder) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(callOrder), callOrder)
	}

	// Now emit again - handler 3 should now be called (added in first emit)
	// Plus handler 1 will add yet another handler (but won't be called this emit)
	callOrder = nil
	e.Emit(testEvent{Value: 2})

	// handlers 1, 2, and 3 (added during first emit)
	if len(callOrder) != 3 {
		t.Errorf("expected 3 calls on second emit, got %d: %v", len(callOrder), callOrder)
	}
}

func TestEmitter_ConcurrentRegistration(t *testing.T) {
	var e Emitter[testEvent]

	var wg sync.WaitGroup
	var callCount atomic.Int32

	// Concurrently register handlers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.OnEvent(func(_ testEvent) {
				callCount.Add(1)
			})
		}()
	}

	wg.Wait()

	// Emit and check all handlers were called
	e.Emit(testEvent{Value: 1})

	if callCount.Load() != 50 {
		t.Errorf("expected 50 handler calls, got %d", callCount.Load())
	}
}

func TestEmitter_ConcurrentEmission(t *testing.T) {
	var e Emitter[testEvent]

	var callCount atomic.Int32
	e.OnEvent(func(_ testEvent) {
		callCount.Add(1)
	})

	var wg sync.WaitGroup

	// Concurrently emit events
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			e.Emit(testEvent{Value: v})
		}(i)
	}

	wg.Wait()

	if callCount.Load() != 100 {
		t.Errorf("expected 100 emissions, got %d", callCount.Load())
	}
}

func TestEmitter_ConcurrentRegistrationAndEmission(t *testing.T) {
	var e Emitter[testEvent]

	var wg sync.WaitGroup

	// Concurrently register and emit
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			e.OnEvent(func(_ testEvent) {})
		}()
		go func(v int) {
			defer wg.Done()
			e.Emit(testEvent{Value: v})
		}(i)
	}

	wg.Wait()
	// Test passes if no race conditions or panics occur
}

func TestEmitter_StringEvent(t *testing.T) {
	var e Emitter[string]

	var received string
	e.OnEvent(func(s string) {
		received = s
	})

	e.Emit("hello")

	if received != "hello" {
		t.Errorf("expected 'hello', got '%s'", received)
	}
}

func TestEmitter_StructEvent(t *testing.T) {
	type complexEvent struct {
		ID      string
		Payload map[string]int
	}

	var e Emitter[complexEvent]

	var received complexEvent
	e.OnEvent(func(ev complexEvent) {
		received = ev
	})

	e.Emit(complexEvent{
		ID:      "test-123",
		Payload: map[string]int{"a": 1, "b": 2},
	})

	if received.ID != "test-123" {
		t.Errorf("expected ID 'test-123', got '%s'", received.ID)
	}
	if received.Payload["a"] != 1 {
		t.Errorf("expected payload['a'] = 1, got %d", received.Payload["a"])
	}
}
