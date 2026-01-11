package backend

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu       sync.RWMutex
	backends = make(map[string]Backend)
)

// Register adds a backend to the registry.
func Register(name string, b Backend) {
	mu.Lock()
	defer mu.Unlock()
	backends[name] = b
}

// Get returns a backend by name.
func Get(name string) (Backend, error) {
	mu.RLock()
	defer mu.RUnlock()
	b, ok := backends[name]
	if !ok {
		return nil, fmt.Errorf("unknown backend: %s", name)
	}
	return b, nil
}

// List returns all registered backend names.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
