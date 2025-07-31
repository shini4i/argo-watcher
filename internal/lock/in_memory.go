package lock

import "sync"

// mutexMap is a thread-safe map to hold mutexes for different keys.
type mutexMap struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

// newMutexMap creates a new, initialized mutexMap.
func newMutexMap() *mutexMap {
	return &mutexMap{
		m: make(map[string]*sync.Mutex),
	}
}

// get returns the mutex for a given key, creating it if it doesn't exist.
func (m *mutexMap) get(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mu, ok := m.m[key]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	m.m[key] = mu
	return mu
}

// InMemoryLocker is an implementation of the Locker interface that uses
// an in-memory mutex map.
type InMemoryLocker struct {
	mutexMap *mutexMap
}

// NewInMemoryLocker creates a new instance of InMemoryLocker.
func NewInMemoryLocker() Locker {
	return &InMemoryLocker{
		mutexMap: newMutexMap(),
	}
}

// WithLock acquires a lock for the given key, executes the function, and releases the lock.
func (l *InMemoryLocker) WithLock(key string, f func() error) error {
	mutex := l.mutexMap.get(key)
	mutex.Lock()
	defer mutex.Unlock()
	return f()
}
