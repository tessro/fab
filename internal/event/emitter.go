// Package event provides generic event emission utilities.
package event

import "sync"

// Emitter provides thread-safe event emission with handler registration.
// It handles the common pattern of registering handlers and emitting events
// to all registered handlers safely.
type Emitter[E any] struct {
	// +checklocks:mu
	handlers []func(E)
	mu       sync.RWMutex
}

// OnEvent registers an event handler.
// Handlers are called synchronously when events are emitted.
func (e *Emitter[E]) OnEvent(handler func(E)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers = append(e.handlers, handler)
}

// Emit sends an event to all registered handlers.
// Handlers are called with a copy of the handler slice to allow
// safe iteration even if new handlers are registered during emission.
// Must not be called with lock held.
func (e *Emitter[E]) Emit(event E) {
	e.mu.RLock()
	handlers := make([]func(E), len(e.handlers))
	copy(handlers, e.handlers)
	e.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}
