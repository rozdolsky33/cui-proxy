package adapter

import (
	"fmt"
	"sync"
)

var (
	mu       sync.RWMutex
	adapters = map[string]Adapter{}
)

// Register makes a available by its Name. It panics if the name is empty or
// already registered.
func Register(a Adapter) {
	mu.Lock()
	defer mu.Unlock()
	name := a.Name()
	if name == "" {
		panic("adapter: Register with empty name")
	}
	if _, dup := adapters[name]; dup {
		panic(fmt.Sprintf("adapter: Register called twice for %q", name))
	}
	adapters[name] = a
}

// Get returns the adapter registered under name.
func Get(name string) (Adapter, bool) {
	mu.RLock()
	defer mu.RUnlock()
	a, ok := adapters[name]
	return a, ok
}
